import { HttpResponse } from 'msw'
import type { OcmDescriptor, OcmReference } from '../api/ocm'

export function desc(name: string, version: string, refs: Array<[string, string, string]> = [], extra: Partial<OcmDescriptor['component']> = {}): OcmDescriptor {
  const componentReferences: OcmReference[] = refs.map(([refName, componentName, v]) => ({ name: refName, componentName, version: v }))
  return { meta: { schemaVersion: 'v2' }, component: { name, version, provider: 'ocm.software', componentReferences, ...extra } }
}

// product → backend, frontend, cache; backend → cache (CLI order, deduplicated)
export const howTo: OcmDescriptor[] = [
  desc('ocm.software/how-to/product', '2.0.0', [
    ['backend', 'ocm.software/how-to/backend', '3.1.0'],
    ['frontend', 'ocm.software/how-to/frontend', '1.5.0'],
    ['cache', 'ocm.software/how-to/cache', '2.0.0'],
  ]),
  desc('ocm.software/how-to/backend', '3.1.0', [['cache', 'ocm.software/how-to/cache', '2.0.0']], {
    sources: [{ name: 'src', version: '3.1.0', type: 'git', access: { type: 'gitHub', repoUrl: 'https://github.com/acme/backend', commit: 'a1b2c3' } }],
    resources: [
      { name: 'api-image', version: '3.1.0', type: 'ociImage', access: { type: 'ociArtifact', imageReference: 'ghcr.io/acme/api:3.1.0' }, extraIdentity: { os: 'linux', arch: 'amd64' } },
      { name: 'api-image', version: '3.1.0', type: 'ociImage', access: { type: 'ociArtifact', imageReference: 'ghcr.io/acme/api:3.1.0' }, extraIdentity: { os: 'linux', arch: 'arm64' } },
      { name: 'chart', version: '3.1.0', type: 'helmChart' },
    ],
  }),
  desc('ocm.software/how-to/cache', '2.0.0'),
  desc('ocm.software/how-to/frontend', '1.5.0'),
]

export const located = (descs: OcmDescriptor[], location = 'oci::ghcr.io/open-component-model') => descs.map((descriptor) => ({ location, descriptor }))

// Splits /v1/components/{name}[/descriptors/{v}|/versions|...] (name percent-encoded).
export function parseApiPath(url: string): { name: string; kind: 'component' | 'versions' | 'descriptor' | 'resource' | 'overview'; version?: string } {
  const [, , , name = '', kind = '', version] = new URL(url).pathname.split('/')
  return {
    name: decodeURIComponent(name),
    kind: kind === 'versions' ? 'versions' : kind === 'descriptors' ? 'descriptor' : kind === 'resources' ? 'resource' : kind === 'overview' ? 'overview' : 'component',
    version,
  }
}

export const componentJson = (name: string, extra: object = {}) => ({
  name, title: name, latest: '2.0.0', defaultVersion: '2.0.0', signed: false, referencedBy: [], referencedByCount: 0, versionCount: 1, locations: [{ repository: 'oci::ghcr.io/acme/ocm', versionCount: 1 }], sources: [{ key: 'github:org:acme', title: 'Acme', type: 'github/v1', locations: ['oci::ghcr.io/acme/ocm'], groups: [] }], links: { versions: '' }, ...extra,
})

// The overview of a version that has none: keeps the Docs tab hidden in tests that do not care about it.
export const overviewMissing = () => HttpResponse.json({ errors: [{ code: 'VERSION_UNKNOWN', message: 'nope', detail: {} }] }, { status: 404 })

// The attestations of a version that the sync did not gather (404 NOT_COLLECTED).
export const attestationsNotCollected = (collected: { version: string; repository: string }[] = [{ version: '2.0.0', repository: 'oci::ghcr.io/acme/ocm' }]) =>
  HttpResponse.json({ errors: [{ code: 'NOT_COLLECTED', message: 'not collected', detail: { collected } }] }, { status: 404 })
