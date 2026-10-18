import { textKind } from './resourceText'
import { absoluteOnly } from '../components/ResourceText'

test('textKind picks markdown, text or download', () => {
  expect(textKind('text/markdown', 'changelog', 10)).toBe('markdown')
  expect(textKind('application/octet-stream', 'changelog', 10)).toBe('markdown') // no media type
  expect(textKind('text/plain; charset=utf-8', 'notes', 10)).toBe('text')
  expect(textKind('application/pdf', 'CHANGELOG.md', 10)).toBe('markdown') // *.md wins
  expect(textKind('application/pdf', 'changes', 10)).toBe('download')
  expect(textKind('text/markdown', 'changelog', 2 << 20)).toBe('download')
})

test('absoluteOnly keeps http(s) URLs only', () => {
  expect(absoluteOnly('https://example.com/a')).toBe('https://example.com/a')
  expect(absoluteOnly('HTTP://example.com')).toBe('HTTP://example.com')
  expect(absoluteOnly('./img.png')).toBeNull()
  expect(absoluteOnly('#section')).toBeNull()
  expect(absoluteOnly('javascript:alert(1)')).toBeNull()
})
