import { howTo } from '../test/fixtures'
import { contentItems, identityString, sourceUrl, typeCounts } from './contents'

test('sources first, then resources, unique keys', () => {
  const items = contentItems(howTo[1])
  expect(items.map((i) => `${i.kind}:${i.element.name}`)).toEqual(['source:src', 'resource:api-image', 'resource:api-image', 'resource:chart'])
  expect(new Set(items.map((i) => i.key)).size).toBe(4)
})

test('type counts, most frequent first', () => {
  expect(typeCounts(contentItems(howTo[1]))).toEqual([['ociImage', 2], ['helmChart', 1]])
})

test('identity and source links', () => {
  expect(identityString({ os: 'linux', arch: 'amd64' })).toBe('arch=amd64, os=linux')
  expect(identityString(null)).toBe('')
})

test('a descriptor without resources or sources has no contents', () => {
  expect(contentItems(howTo[2])).toEqual([])
})

const src = (access: Record<string, unknown>) => sourceUrl({ name: 's', version: '1', access: access as never })

test('source URL with commit', () => {
  expect(sourceUrl(howTo[1].component.sources![0])).toBe('https://github.com/acme/backend/tree/a1b2c3')
  expect(src({ repoUrl: 'https://github.com/acme/x' })).toBe('https://github.com/acme/x')
  expect(src({ repoUrl: 'https://gitlab.com/acme/x', commit: 'abc' })).toBe('https://gitlab.com/acme/x/-/tree/abc')
  expect(src({ repoUrl: 'https://my-gitlab.corp/acme/x', commit: 'abc' })).toBe('https://my-gitlab.corp/acme/x/-/tree/abc')
  expect(src({ repoUrl: 'https://git.example.com/acme/x', commit: 'abc' })).toBe('https://git.example.com/acme/x')
})

test('source URL normalization', () => {
  expect(src({ repoUrl: 'git@github.com:acme/x.git', commit: 'c1' })).toBe('https://github.com/acme/x/tree/c1')
  expect(src({ repoUrl: 'github.com/acme/x' })).toBe('https://github.com/acme/x')
  expect(src({ repoUrl: 'https://github.com/acme/x.git/' })).toBe('https://github.com/acme/x')
  expect(src({ repository: 'github.com/acme/y' })).toBe('https://github.com/acme/y')
  expect(src({ url: 'https://github.com/acme/z' })).toBe('https://github.com/acme/z')
  expect(src({ repoUrl: 'ftp://github.com/acme/x' })).toBeUndefined()
  expect(src({})).toBeUndefined()
  expect(sourceUrl({ name: 's', version: '1' })).toBeUndefined()
})

test('commit is URL-encoded in the source URL', () => {
  expect(src({ repoUrl: 'https://github.com/acme/x', commit: 'feature/a#b' })).toBe('https://github.com/acme/x/tree/feature%2Fa%23b')
  expect(src({ repoUrl: 'https://gitlab.com/acme/x', commit: 'a/b' })).toBe('https://gitlab.com/acme/x/-/tree/a%2Fb')
})
