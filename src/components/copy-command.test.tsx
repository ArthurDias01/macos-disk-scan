import { expect, test } from 'bun:test'
import { renderToString } from 'react-dom/server'
import { CopyCommand } from '~/components/copy-command'

test('icon variant renders no wrapping label', () => {
  const html = renderToString(<CopyCommand path="/Users/arthur/Library" />)
  expect(html).toContain('svg')
  expect(html).not.toContain('Trash cmd')
  expect(html).toContain('whitespace-nowrap')
})

test('labelled variant keeps its text', () => {
  const html = renderToString(<CopyCommand path="/Users/arthur" variant="labelled" />)
  expect(html).toContain('Trash cmd')
})
