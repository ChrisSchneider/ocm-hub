import { labelValue } from './labels'

test('labelValue: scalars and scalar arrays inline, the rest json', () => {
  expect(labelValue('a')).toEqual({ kind: 'inline', text: 'a' })
  expect(labelValue(3)).toEqual({ kind: 'inline', text: '3' })
  expect(labelValue(true)).toEqual({ kind: 'inline', text: 'true' })
  expect(labelValue(['a', 1])).toEqual({ kind: 'inline', text: 'a, 1' })
  expect(labelValue({ a: 1 })).toEqual({ kind: 'json', value: { a: 1 } })
  expect(labelValue([{ a: 1 }])).toEqual({ kind: 'json', value: [{ a: 1 }] })
  expect(labelValue(NaN).kind).toBe('json')
})
