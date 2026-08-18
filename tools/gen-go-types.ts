/**
 * Generate Go type declarations from `shared/schema.ts`.
 *
 * The TypeScript file stays canonical: it is what the SPA compiles against, and
 * the snapshot shape is ultimately a browser concern. The Go scanner reads the
 * same declarations through this generator, so the two can never drift — a
 * check in CI regenerates and fails on any diff.
 *
 * Only declarations are translated. Functions in `schema.ts` are logic, not
 * shape, and are hand-ported into `internal/schema/histogram.go` where they can
 * carry their own tests.
 *
 * Numbers map to `int64` by default, because nearly every number in this schema
 * is a byte count or a tally. The handful that are genuinely fractional carry a
 * `@go float64` JSDoc tag in `schema.ts`.
 */

import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import ts from 'typescript'

const SOURCE = resolve(import.meta.dir, '../shared/schema.ts')
const TARGET = resolve(import.meta.dir, '../internal/schema/schema_gen.go')
/** Editor autocomplete and validation for `scan.config.json`. */
const SCHEMA_TARGET = resolve(import.meta.dir, '../scan.config.schema.json')

const sourceText = readFileSync(SOURCE, 'utf8')
const sourceFile = ts.createSourceFile(
  SOURCE,
  sourceText,
  ts.ScriptTarget.Latest,
  /* setParentNodes */ true,
)

// ---------------------------------------------------------------------------
// Comments
// ---------------------------------------------------------------------------

interface Doc {
  lines: string[]
  /** Explicit Go type from a `@go <type>` tag. */
  goType?: string
}

function readDoc(node: ts.Node): Doc {
  const ranges = ts.getLeadingCommentRanges(sourceText, node.getFullStart()) ?? []
  const last = ranges[ranges.length - 1]
  if (!last) return { lines: [] }

  const raw = sourceText.slice(last.pos, last.end)
  // Only a JSDoc block documents a declaration. The bare `// ----` rules in
  // schema.ts divide sections; carrying them over would title every type with
  // a row of dashes.
  if (!raw.startsWith('/**')) return { lines: [] }

  const lines: string[] = []
  let goType: string | undefined

  for (const line of raw.split('\n')) {
    const cleaned = line
      .replace(/^\s*\/\*\*?/, '')
      .replace(/\*\/\s*$/, '')
      .replace(/^\s*\*/, '')
      .trim()

    const tag = cleaned.match(/@go\s+(\S+)/)
    if (tag) {
      goType = tag[1]
      // A line that is only the tag carries no prose worth keeping.
      const remainder = cleaned.replace(/@go\s+\S+\s*/, '').replace(/^[—-]\s*/, '').trim()
      if (remainder) lines.push(remainder)
      continue
    }
    lines.push(cleaned)
  }

  while (lines.length > 0 && lines[0] === '') lines.shift()
  while (lines.length > 0 && lines[lines.length - 1] === '') lines.pop()

  return { lines, goType }
}

function emitDoc(out: string[], doc: Doc, indent: string, name?: string): void {
  doc.lines.forEach((line, index) => {
    const prefix = index === 0 && name ? `${name} — ` : ''
    out.push(`${indent}// ${prefix}${line}`.trimEnd())
  })
}

// ---------------------------------------------------------------------------
// Naming
// ---------------------------------------------------------------------------

/** Go initialisms that a naive capitalize would get wrong. */
const INITIALISMS = new Set(['id', 'url', 'api', 'json'])

function exportedName(name: string): string {
  if (INITIALISMS.has(name.toLowerCase())) return name.toUpperCase()
  return name.charAt(0).toUpperCase() + name.slice(1)
}

/** `SCHEMA_VERSION` -> `SchemaVersion`, `CATEGORIES` -> `Categories`. */
function pascalFromScreamingSnake(name: string): string {
  return name
    .toLowerCase()
    .split('_')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join('')
}

function pascalFromLiteral(value: string): string {
  return value
    .split(/[^a-zA-Z0-9]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join('')
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

function isNullish(node: ts.TypeNode): boolean {
  if (node.kind === ts.SyntaxKind.UndefinedKeyword) return true
  return ts.isLiteralTypeNode(node) && node.literal.kind === ts.SyntaxKind.NullKeyword
}

function goType(node: ts.TypeNode, override?: string): string {
  if (ts.isTypeOperatorNode(node)) return goType(node.type, override)
  if (ts.isParenthesizedTypeNode(node)) return goType(node.type, override)

  switch (node.kind) {
    case ts.SyntaxKind.StringKeyword:
      return 'string'
    case ts.SyntaxKind.BooleanKeyword:
      return 'bool'
    case ts.SyntaxKind.NumberKeyword:
      return override ?? 'int64'
  }

  if (ts.isArrayTypeNode(node)) {
    return `[]${goType(node.elementType, override)}`
  }

  if (ts.isUnionTypeNode(node)) {
    const concrete = node.types.filter((member) => !isNullish(member))
    const nullable = concrete.length !== node.types.length
    if (concrete.length !== 1) {
      throw new Error(`Unsupported union at ${describe(node)}`)
    }
    // `VolumeInfo | null` becomes a pointer: absent is a state the SPA reads.
    return `${nullable ? '*' : ''}${goType(concrete[0], override)}`
  }

  if (ts.isTypeReferenceNode(node)) {
    const name = node.typeName.getText(sourceFile)
    const args = node.typeArguments ?? []

    if (name === 'Record' && args.length === 2) {
      return `map[${goType(args[0], override)}]${goType(args[1], override)}`
    }
    if ((name === 'Array' || name === 'ReadonlyArray') && args.length === 1) {
      return `[]${goType(args[0], override)}`
    }
    if (args.length > 0) {
      throw new Error(`Unsupported generic ${name} at ${describe(node)}`)
    }
    return name
  }

  throw new Error(`Unsupported type at ${describe(node)}`)
}

function describe(node: ts.Node): string {
  const { line } = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile))
  return `${SOURCE}:${line + 1}`
}

// ---------------------------------------------------------------------------
// Emitters
// ---------------------------------------------------------------------------

const out: string[] = [
  '// Code generated by tools/gen-go-types.ts. DO NOT EDIT.',
  '//',
  '// Source: shared/schema.ts — the canonical snapshot schema, shared with the SPA.',
  '// Regenerate with `bun run gen:types`.',
  '',
  'package schema',
  '',
]

function emitInterface(node: ts.InterfaceDeclaration): void {
  const doc = readDoc(node)
  const name = node.name.text
  emitDoc(out, doc, '', name)
  out.push(`type ${name} struct {`)

  for (const member of node.members) {
    if (!ts.isPropertySignature(member) || !member.type) {
      throw new Error(`Unsupported member at ${describe(member)}`)
    }
    const memberDoc = readDoc(member)
    const tsName = member.name.getText(sourceFile).replace(/^['"]|['"]$/g, '')
    const optional = member.questionToken !== undefined
    const base = goType(member.type, memberDoc.goType)
    // An optional field is absent, not zero: `duplicateCopies: 0` would claim a
    // group of nothing. Pointers keep absent and zero distinguishable.
    const type = optional && !base.startsWith('*') && !base.startsWith('[]') ? `*${base}` : base
    const tag = optional ? `${tsName},omitempty` : tsName

    if (memberDoc.lines.length > 0) {
      if (out[out.length - 1] !== `type ${name} struct {`) out.push('')
      emitDoc(out, memberDoc, '\t')
    }
    out.push(`\t${exportedName(tsName)} ${type} \`json:"${tag}"\``)
  }

  out.push('}', '')
}

function emitStringUnion(node: ts.TypeAliasDeclaration): void {
  const doc = readDoc(node)
  const name = node.name.text
  const union = node.type

  if (!ts.isUnionTypeNode(union)) {
    out.push(`type ${name} = ${goType(union, doc.goType)}`, '')
    return
  }

  const values = union.types.map((member) => {
    if (!ts.isLiteralTypeNode(member) || !ts.isStringLiteral(member.literal)) {
      throw new Error(`Unsupported union member at ${describe(member)}`)
    }
    return member.literal.text
  })

  emitDoc(out, doc, '', name)
  out.push(`type ${name} string`, '', 'const (')
  for (const value of values) {
    out.push(`\t${name}${pascalFromLiteral(value)} ${name} = ${JSON.stringify(value)}`)
  }
  out.push(')', '')
}

function emitVariable(statement: ts.VariableStatement): void {
  const doc = readDoc(statement)

  for (const declaration of statement.declarationList.declarations) {
    const tsName = declaration.name.getText(sourceFile)
    const name = pascalFromScreamingSnake(tsName)
    const initializer = declaration.initializer
    if (!initializer) continue

    if (ts.isNumericLiteral(initializer)) {
      emitDoc(out, doc, '', name)
      out.push(`const ${name} = ${initializer.text}`, '')
      continue
    }
    if (ts.isStringLiteral(initializer)) {
      emitDoc(out, doc, '', name)
      out.push(`const ${name} = ${JSON.stringify(initializer.text)}`, '')
      continue
    }
    if (ts.isArrayLiteralExpression(initializer) && declaration.type) {
      const elementType = goType(declaration.type).replace(/^\[\]/, '')
      const values = initializer.elements.map((element) => {
        if (!ts.isStringLiteral(element)) {
          throw new Error(`Unsupported array element at ${describe(element)}`)
        }
        return `\t${elementType}${pascalFromLiteral(element.text)},`
      })
      emitDoc(out, doc, '', name)
      out.push(`var ${name} = []${elementType}{`, ...values, '}', '')
      continue
    }

    throw new Error(`Unsupported declaration ${tsName} at ${describe(declaration)}`)
  }
}

// ---------------------------------------------------------------------------
// JSON Schema for scan.config.json
// ---------------------------------------------------------------------------

/**
 * The Go scanner cannot read `scan.config.ts`, so config moves to JSON. A
 * `$schema` pointer buys back what the TypeScript file gave: autocomplete and
 * validation in the editor, with no build step between editing a number and
 * running a scan.
 *
 * Every property is optional — the defaults are a complete configuration on
 * their own, and a config file exists to move one or two of them.
 */
function jsonSchemaType(node: ts.TypeNode): Record<string, unknown> {
  if (ts.isTypeOperatorNode(node)) return jsonSchemaType(node.type)

  switch (node.kind) {
    case ts.SyntaxKind.StringKeyword:
      return { type: 'string' }
    case ts.SyntaxKind.BooleanKeyword:
      return { type: 'boolean' }
    case ts.SyntaxKind.NumberKeyword:
      return { type: 'integer' }
  }

  if (ts.isArrayTypeNode(node)) {
    return { type: 'array', items: jsonSchemaType(node.elementType) }
  }

  if (ts.isTypeReferenceNode(node)) {
    const name = node.typeName.getText(sourceFile)
    const args = node.typeArguments ?? []

    if (name === 'Record' && args.length === 2) {
      return { type: 'object', additionalProperties: jsonSchemaType(args[1]) }
    }
    if (name === 'Category') return { enum: categoryValues }
  }

  throw new Error(`Unsupported config type at ${describe(node)}`)
}

let categoryValues: string[] = []

function emitConfigSchema(node: ts.InterfaceDeclaration): void {
  const properties: Record<string, unknown> = {
    $schema: { type: 'string', description: 'Path to this schema, for editor support.' },
  }

  for (const member of node.members) {
    if (!ts.isPropertySignature(member) || !member.type) continue
    const name = member.name.getText(sourceFile)
    const doc = readDoc(member)
    properties[name] = {
      ...jsonSchemaType(member.type),
      ...(doc.lines.length > 0 ? { description: doc.lines.join(' ') } : {}),
    }
  }

  writeFileSync(
    SCHEMA_TARGET,
    `${JSON.stringify(
      {
        $schema: 'http://json-schema.org/draft-07/schema#',
        title: 'ScanConfig',
        description:
          'Generated from shared/schema.ts by tools/gen-go-types.ts. Do not edit.',
        type: 'object',
        additionalProperties: false,
        properties,
      },
      null,
      2,
    )}\n`,
  )
}

// Categories have to be known before the config schema references them.
for (const statement of sourceFile.statements) {
  if (ts.isTypeAliasDeclaration(statement) && statement.name.text === 'Category') {
    const union = statement.type
    if (ts.isUnionTypeNode(union)) {
      categoryValues = union.types.map((member) =>
        ts.isLiteralTypeNode(member) && ts.isStringLiteral(member.literal)
          ? member.literal.text
          : '',
      )
    }
  }
}

for (const statement of sourceFile.statements) {
  const exported = ts
    .getModifiers(statement as ts.HasModifiers)
    ?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword)
  if (!exported) continue

  if (ts.isInterfaceDeclaration(statement)) {
    emitInterface(statement)
    if (statement.name.text === 'ScanConfig') emitConfigSchema(statement)
  } else if (ts.isTypeAliasDeclaration(statement)) emitStringUnion(statement)
  else if (ts.isVariableStatement(statement)) emitVariable(statement)
  // Functions are logic, not shape: hand-ported with tests.
}

mkdirSync(dirname(TARGET), { recursive: true })
writeFileSync(TARGET, `${out.join('\n').replace(/\n{3,}/g, '\n\n').trimEnd()}\n`)

// gofmt owns struct-tag alignment, so the output is canonical and the drift
// check in CI compares like with like.
const formatted = Bun.spawnSync(['gofmt', '-w', TARGET])
if (formatted.exitCode !== 0) {
  throw new Error(`gofmt failed: ${formatted.stderr.toString()}`)
}

console.log(`generated ${TARGET}`)
