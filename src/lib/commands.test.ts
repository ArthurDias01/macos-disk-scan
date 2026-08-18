import { describe, expect, test } from 'bun:test'
import { buildCommand, buildScript, collidingBasenames } from './commands'

const paths = [
  '/Users/arthur/Documents/Backup 2024',
  "/Users/arthur/Movies/Arthur's clip.mov",
  '/Users/arthur/.npm',
]

describe('buildCommand', () => {
  test('moves the whole selection in one command', () => {
    expect(buildCommand('trash', paths)).toBe(
      "mv -n '/Users/arthur/Documents/Backup 2024' " +
        "'/Users/arthur/Movies/Arthur'\\''s clip.mov' " +
        "'/Users/arthur/.npm' ~/.Trash/",
    )
  })

  test('quotes spaces and apostrophes, which macOS paths are full of', () => {
    const command = buildCommand('remove', ["/Users/arthur/Arthur's Mac"])
    expect(command).toBe("rm -rf '/Users/arthur/Arthur'\\''s Mac'")
    // The apostrophe must not close the quote and leave the rest bare.
    expect(command.endsWith("Mac'")).toBe(true)
  })

  test('reveal takes every path at once', () => {
    expect(buildCommand('reveal', ['/a', '/b'])).toBe("open -R '/a' '/b'")
  })

  test('paths are copied raw, one per line', () => {
    expect(buildCommand('paths', ['/a', '/b'])).toBe('/a\n/b')
  })

  test('an empty selection produces nothing to paste', () => {
    expect(buildCommand('trash', [])).toBe('')
  })
})

describe('buildScript', () => {
  const script = buildScript(
    [
      { path: '/Users/arthur/a', size: 1 },
      { path: '/Users/arthur/b b', size: 2 },
    ],
    '3 B',
  )

  test('is a reviewable shell script with one move per line', () => {
    expect(script.startsWith('#!/bin/sh')).toBe(true)
    expect(script).toContain("mv -n '/Users/arthur/a' ~/.Trash/")
    expect(script).toContain("mv -n '/Users/arthur/b b' ~/.Trash/")
  })

  test('records what the selection was worth', () => {
    expect(script).toContain('2 item(s), 3 B')
  })

  test('says the Trash still uses disk', () => {
    expect(script).toContain('keep using disk')
  })

  test('records each successful move in the ledger the next scan reads', () => {
    // `&&` matters: a path is only logged if its move actually succeeded.
    expect(script).toContain("mv -n '/Users/arthur/a' ~/.Trash/ && printf")
    expect(script).toContain('LEDGER=')
    expect(script).toContain('>> "$LEDGER"')
  })

  test('does not abort the whole run on one failure', () => {
    expect(script).not.toContain('set -e')
  })
})

describe('collidingBasenames', () => {
  test('finds names that would silently collide in the Trash', () => {
    expect(
      collidingBasenames(['/a/node_modules', '/b/node_modules', '/c/other']),
    ).toEqual(['node_modules'])
  })

  test('is quiet when every name is distinct', () => {
    expect(collidingBasenames(['/a/one', '/b/two'])).toEqual([])
  })
})
