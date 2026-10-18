import { collapseMinors } from './kubernetes'
test('collapses consecutive minors into ranges', () => {
  expect(collapseMinors(['1.28', '1.29', '1.31', '1.32'])).toBe('1.28–1.29, 1.31–1.32')
  expect(collapseMinors(['1.30'])).toBe('1.30')
  expect(collapseMinors(['1.29', '1.31'])).toBe('1.29, 1.31')
  expect(collapseMinors([])).toBe('')
})
