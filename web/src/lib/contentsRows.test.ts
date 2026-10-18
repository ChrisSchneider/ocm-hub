import type { OcmElement } from '../api/ocm'
import { childName, groupItems, resourceViewer } from './contentsRows'
import type { ContentItem } from './contents'

const res = (name: string, version: string, extra?: Record<string, string>, type = 'ociImage'): ContentItem => ({
  kind: 'resource',
  key: `resource/${name}/${version}/${JSON.stringify(extra)}`,
  element: { name, version, type, extraIdentity: extra },
})

test('groupItems: three executables differing in os/arch become one group with three children', () => {
  const src: ContentItem = { kind: 'source', key: 'source/0', element: { name: 's', version: '1' } }
  const a = res('cli', '1', { os: 'linux', architecture: 'amd64' }, 'executable')
  const b = res('cli', '1', { os: 'linux', architecture: 'arm64' }, 'executable')
  const mid = res('other', '1')
  const c = res('cli', '1', { os: 'darwin', architecture: 'arm64' }, 'executable')
  const rows = groupItems([src, a, mid, b, c])
  expect(rows).toHaveLength(3)
  expect(rows[0]).toBe(src)
  expect(rows[1]).toEqual({ kind: 'group', key: 'group/cli/executable/1', name: 'cli', type: 'executable', version: '1', children: [a, b, c] })
  expect(rows[2]).toBe(mid)
})

test('groupItems: single member after filtering is not a group', () => {
  const a = res('cli', '1', { os: 'linux' })
  expect(groupItems([a])).toEqual([a])
})

test('groupItems: same name, different version stays separate', () => {
  const a = res('cli', '1')
  const b = res('cli', '2')
  expect(groupItems([a, b])).toEqual([a, b])
})

test('childName', () => {
  expect(childName({ os: 'linux', architecture: 'amd64' })).toBe('linux/amd64')
  expect(childName({ variant: 'fips' })).toBe('variant=fips')
})

test('resourceViewer', () => {
  const el = (type: string, mediaType?: string): OcmElement => ({ name: 'x', version: '1', type, access: mediaType ? { mediaType } : {} })
  expect(resourceViewer(el('sbom'))).toBe('sbom')
  expect(resourceViewer(el('sbom', 'application/vnd.cyclonedx+json'))).toBe('sbom')
  expect(resourceViewer(el('sbom', 'application/vnd.cyclonedx+xml'))).toBeUndefined()
  expect(resourceViewer(el('oscal', 'application/json'))).toBeUndefined()
})
