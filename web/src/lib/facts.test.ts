import { formatBytes, signatureLabel, sourceLink, totalSize } from './facts'

const sig = (algorithm: string, mediaType: string, issuer?: string) =>
  ({ name: 's', digest: { hashAlgorithm: 'SHA-256', normalisationAlgorithm: 'x', value: 'x' }, signature: { algorithm, value: 'v', mediaType, issuer } })

test('signatureLabel names the algorithm and tells certificate from sigstore', () => {
  expect(signatureLabel([sig('RSASSA-PSS', 'application/vnd.ocm.signature.rsa')])).toBe('RSASSA-PSS')
  expect(signatureLabel([sig('RSASSA-PSS', 'application/x-pem-file')])).toBe('RSASSA-PSS · certificate')
  expect(signatureLabel([sig('RSASSA-PKCS1-V1_5', 'application/vnd.ocm.signature.rsa', 'CN=acme')])).toBe('RSASSA-PKCS1-V1_5 · certificate')
  expect(signatureLabel([sig('sigstore', 'application/vnd.dev.sigstore.bundle.v0.3+json')])).toBe('sigstore')
  expect(signatureLabel([sig('RSASSA-PSS', 'application/x-pem-file'), sig('RSASSA-PSS', 'application/x-pem-file')])).toBe('RSASSA-PSS · certificate')
  expect(signatureLabel([])).toBe('')
})

test('sourceLink uses the first http(s) repository URL and shortens the commit', () => {
  const src = (access: Record<string, unknown>) => ({ name: 'src', version: '1', type: 'git', access })
  expect(sourceLink([src({ type: 'gitHub', repoUrl: 'https://github.com/acme/app.git', commit: '0123456789' })])).toEqual({ label: 'github.com/acme/app @ 0123456', href: 'https://github.com/acme/app/tree/0123456789' })
  expect(sourceLink([src({ type: 'git', repository: 'https://example.com/acme/app/' })])).toEqual({ label: 'example.com/acme/app', href: 'https://example.com/acme/app' })
  expect(sourceLink([src({ type: 'git', repository: 'git@github.com:acme/app.git' })])).toBeUndefined()
  expect(sourceLink([])).toBeUndefined()
})

test('totalSize only when every resource has a numeric size; formatBytes uses decimal units', () => {
  const r = (access: Record<string, unknown>) => ({ name: 'a', version: '1', type: 'blob', relation: 'local' as const, access })
  expect(totalSize([r({ size: 1000 }), r({ size: 500 })])).toBe(1500)
  expect(totalSize([r({ size: 1000 }), r({ type: 'ociArtifact' })])).toBeUndefined()
  expect(totalSize([])).toBeUndefined()
  expect(formatBytes(512)).toBe('512 B')
  expect(formatBytes(1500)).toBe('1.5 kB')
  expect(formatBytes(12_300_000)).toBe('12 MB')
})
