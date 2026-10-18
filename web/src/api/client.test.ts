import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { ApiError, attestationContentUrl, getComponent, getDescriptor, getDescriptorTree, getDocument, getVersions, imageRawUrl, listComponents, resourceUrl } from './client'
import { descId, ocmRef, parseId, providerName } from './ocm'

test('getComponent encodes the component name', async () => {
  let path = ''
  server.use(http.get('/v1/components/*', ({ request }) => {
    path = new URL(request.url).pathname
    return HttpResponse.json({ name: 'ocm.software/how-to/product', title: 'P', versionCount: 1, locations: [], sources: [], links: { versions: '' } })
  }))
  const c = await getComponent('ocm.software/how-to/product')
  expect(c.title).toBe('P')
  expect(path).toBe('/v1/components/ocm.software%2Fhow-to%2Fproduct')
})

test('listComponents sends filters as query parameters', async () => {
  let q = new URLSearchParams()
  server.use(http.get('/v1/components', ({ request }) => {
    q = new URL(request.url).searchParams
    return HttpResponse.json({ components: [], facets: { connectors: [], providers: [], categories: [] } })
  }))
  const l = await listComponents({ q: 'cli', connector: ['github:org:a', 'github:org:b'], category: ['x'], provider: 'acme', cursor: 'c1' })
  expect(l.components).toEqual([])
  expect(q.get('q')).toBe('cli')
  expect(q.getAll('connector')).toEqual(['github:org:a', 'github:org:b'])
  expect(q.getAll('category')).toEqual(['x'])
  expect(q.get('provider')).toBe('acme')
  expect(q.get('cursor')).toBe('c1')
})

test('errors become ApiError with the server code', async () => {
  server.use(http.get('/v1/components', () => HttpResponse.json({ errors: [{ code: 'UPSTREAM_ERROR', message: 'boom', detail: {} }] }, { status: 502 })))
  await expect(listComponents({})).rejects.toMatchObject({ status: 502, code: 'UPSTREAM_ERROR', message: 'boom' })
  await expect(listComponents({})).rejects.toBeInstanceOf(ApiError)
})

test('getVersions sends semverConstraint and includePrerelease and returns entries', async () => {
  let q = new URLSearchParams()
  let path = ''
  server.use(http.get('/v1/components/*', ({ request }) => {
    const u = new URL(request.url)
    q = u.searchParams
    path = u.pathname
    return HttpResponse.json({ name: 'x', versions: [{ version: '1.1.0', publishedAt: '2026-01-01T00:00:00Z', stable: true, locations: ['oci::a'] }] })
  }))
  const r = await getVersions('acme.org/x', { semverConstraint: '>=1.0.0', includePrerelease: true })
  expect(r.versions.map((v) => v.version)).toEqual(['1.1.0'])
  expect(path).toBe('/v1/components/acme.org%2Fx/versions')
  expect(q.get('semverConstraint')).toBe('>=1.0.0')
  expect(q.get('includePrerelease')).toBe('true')
})

test('getDescriptorTree asks for recursive=-1&compact=true as JSON and returns located descriptors', async () => {
  let url = new URL('http://x')
  let accept = ''
  server.use(http.get('/v1/components/*', ({ request }) => {
    url = new URL(request.url)
    accept = request.headers.get('Accept') ?? ''
    return HttpResponse.json([{ location: 'oci::ghcr.io/a', descriptor: { meta: { schemaVersion: 'v2' }, component: { name: 'acme.org/p', version: '1.0.0', provider: 'acme.org' } } }])
  }))
  const list = await getDescriptorTree('acme.org/p', '1.0.0')
  expect(list).toHaveLength(1)
  expect(list[0].location).toBe('oci::ghcr.io/a')
  expect(url.pathname).toBe('/v1/components/acme.org%2Fp/descriptors/1.0.0')
  expect(url.searchParams.get('recursive')).toBe('-1')
  expect(url.searchParams.get('compact')).toBe('true')
  expect(accept).toContain('application/json')
})

test('getDescriptor returns one descriptor', async () => {
  server.use(http.get('/v1/components/*', () =>
    HttpResponse.json({ meta: { schemaVersion: 'v2' }, component: { name: 'acme.org/p', version: '1.0.0', provider: { name: 'acme.org' } } })))
  const d = await getDescriptor('acme.org/p', '1.0.0')
  expect(providerName(d)).toBe('acme.org')
})

test('helpers', () => {
  expect(resourceUrl('acme.org/p', '1.0.0', 'bin', { os: 'linux', arch: 'amd64' }))
    .toBe('/v1/components/acme.org%2Fp/resources/1.0.0/bin?extraIdentity.os=linux&extraIdentity.arch=amd64')
  expect(resourceUrl('acme.org/p', '1.0.0', 'data')).toBe('/v1/components/acme.org%2Fp/resources/1.0.0/data')
  expect(parseId(descId('acme.org/p', '1.0.0+build'))).toEqual({ name: 'acme.org/p', version: '1.0.0+build' })
  expect(ocmRef('ghcr.io/acme/ocm', 'acme.org/p', '1.0.0')).toBe('ghcr.io/acme/ocm//acme.org/p:1.0.0')
  expect(ocmRef('http://localhost:5000/ocm', 'acme.org/p', '1.0.0')).toBe('oci::http://localhost:5000/ocm//acme.org/p:1.0.0')
})

test('attestationContentUrl encodes the digest and adds as=document and repository', () => {
  expect(attestationContentUrl('acme.org/p', '1.0.0', 'sha256:abc')).toBe('/v1/components/acme.org%2Fp/attestations/1.0.0/sha256%3Aabc')
  const u = new URL(attestationContentUrl('acme.org/p', '1.0.0', 'sha256:abc', 'oci::ghcr.io/acme', true), 'http://x')
  expect(u.pathname).toBe('/v1/components/acme.org%2Fp/attestations/1.0.0/sha256%3Aabc')
  expect(u.searchParams.get('as')).toBe('document')
  expect(u.searchParams.get('repository')).toBe('oci::ghcr.io/acme')
})

test('imageRawUrl names the platform digest and the part', () => {
  expect(imageRawUrl('acme.org/p', '1.0.0', 'img', { os: 'linux' }, 'sha256:abc', 'config'))
    .toBe('/v1/components/acme.org%2Fp/images/1.0.0/img/sha256%3Aabc?extraIdentity.os=linux&raw=config')
})

test('getDocument asks a resource for as=document with its extra identity', async () => {
  let url = new URL('http://x')
  server.use(http.get('/v1/components/*', ({ request }) => {
    url = new URL(request.url)
    return HttpResponse.json({ format: 'CycloneDX 1.6', sbom: { packages: [] } })
  }))
  const d = await getDocument('acme.org/p', '1.0.0', { resource: 'sbom', extraIdentity: { arch: 'amd64' } }, 'oci::ghcr.io/acme')
  expect(d.format).toBe('CycloneDX 1.6')
  expect(url.pathname).toBe('/v1/components/acme.org%2Fp/resources/1.0.0/sbom')
  expect(url.searchParams.get('extraIdentity.arch')).toBe('amd64')
  expect(url.searchParams.get('repository')).toBe('oci::ghcr.io/acme')
  expect(url.searchParams.get('as')).toBe('document')
})

test('getDocument errors carry the code and detail', async () => {
  server.use(http.get('/v1/components/*', () =>
    HttpResponse.json({ errors: [{ code: 'DOCUMENT_CHANGED', message: 'changed', detail: { url: 'https://example.com/a.json' } }] }, { status: 409 })))
  await expect(getDocument('acme.org/p', '1.0.0', { attestation: 'sha256:abc' }))
    .rejects.toMatchObject({ status: 409, code: 'DOCUMENT_CHANGED', detail: { url: 'https://example.com/a.json' } })
})
