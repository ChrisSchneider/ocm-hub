import { formatDate, relativeTime } from './time'

const now = Date.parse('2026-10-03T12:00:00Z')

test('picks a sensible unit', () => {
  expect(relativeTime('2026-10-03T11:30:00Z', now)).toBe('30 minutes ago')
  expect(relativeTime('2026-10-03T09:00:00Z', now)).toBe('3 hours ago')
  expect(relativeTime('2026-10-02T12:00:00Z', now)).toBe('yesterday')
  expect(relativeTime('2026-08-03T12:00:00Z', now)).toBe('2 months ago')
  expect(relativeTime('2024-10-03T12:00:00Z', now)).toBe('2 years ago')
})

test('unparsable input yields undefined', () => {
  expect(relativeTime('not a date', now)).toBeUndefined()
})

test('formatDate gives the UTC date', () => {
  expect(formatDate('2026-02-01T23:59:00Z')).toBe('2026-02-01')
})
