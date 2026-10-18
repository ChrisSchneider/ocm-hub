import { artifactReference, describedResource, sbomResources } from './sbom'

const res = (name: string, type: string, extra: Record<string, unknown> = {}) => ({ name, version: '1.0.0', type, relation: 'local' as const, access: { type: 'LocalBlob/v1' }, ...extra })
const ref = (identitySelector: Record<string, string>) => ({ labels: [{ name: 'ocm.software/artifactReference', version: 'v1', value: { identitySelector } }] })

test('artifactReference reads the identity selector of the label', () => {
  expect(artifactReference(res('sbom', 'sbom', ref({ name: 'image' })))).toEqual({ name: 'image' })
  expect(artifactReference(res('sbom', 'sbom'))).toBeUndefined()
  expect(artifactReference(res('sbom', 'sbom', { labels: [{ name: 'ocm.software/artifactReference', value: 'nonsense' }] }))).toBeUndefined()
})

test('describedResource matches name and extra identity of the selector', () => {
  const amd = res('image', 'ociImage', { extraIdentity: { arch: 'amd64' } })
  const arm = res('image', 'ociImage', { extraIdentity: { arch: 'arm64' } })
  const sbom = res('sbom', 'sbom', ref({ name: 'image', arch: 'arm64' }))
  expect(describedResource(sbom, [amd, arm, sbom])).toBe(arm)
  expect(describedResource(res('sbom', 'sbom', ref({ name: 'missing' })), [amd])).toBeUndefined()
  expect(describedResource(res('sbom', 'sbom'), [amd])).toBeUndefined()
})

test('sbomResources picks the sbom resources', () => {
  expect(sbomResources([res('image', 'ociImage'), res('a', 'sbom'), res('b', 'sbom')]).map((r) => r.name)).toEqual(['a', 'b'])
})
