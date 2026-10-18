import { changelogSources } from './changelog'

test('sources: root first, sub-components with a changelog, foreign marked', () => {
  const d = (name: string, version: string, resources: object[] = []) => ({ meta: { schemaVersion: 'v2' }, component: { name, version, provider: 'acme', resources } })
  const cl = { name: 'changelog', version: '1', type: 'changelog' }
  const got = changelogSources([
    { location: 'oci::a', descriptor: d('acme.org/app', '1.0.0', [cl, { name: 'img', version: '1', type: 'ociImage' }]) as never },
    { location: 'oci::a', descriptor: d('acme.org/none', '1.0.0') as never },
    { location: 'oci::b', descriptor: d('other.org/lib', '2.0.0', [cl]) as never },
  ])
  expect(got.map((s) => [s.component, s.foreign])).toEqual([['acme.org/app', false], ['other.org/lib', true]])
})
