import { parseSearch, stringifySearch } from './search'

test('parseSearch keeps versions as strings', () => {
  expect(parseSearch('?version=1.0&recursive=-1&select=acme.org%2Fx%3A2')).toEqual({ version: '1.0', recursive: '-1', select: 'acme.org/x:2' })
})

test('stringifySearch drops undefined and round-trips', () => {
  const s = stringifySearch({ version: '2', select: undefined, recursive: -1 })
  expect(s).toBe('?version=2&recursive=-1')
  expect(parseSearch(s)).toEqual({ version: '2', recursive: '-1' })
  expect(stringifySearch({})).toBe('')
})
