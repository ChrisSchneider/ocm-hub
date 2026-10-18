import { ocmRef } from '../api/ocm'
import { accessConfig, downloadCommand, identityArg, registryHost, shellQuote, sortResources, transferCommand, verifyConfig } from './usage'

const res = (name: string, type: string, extraIdentity?: Record<string, string>) => ({ name, version: '1.0.0', type, relation: 'local' as const, extraIdentity, access: {} })
const sig = (name: string, algorithm: string) => ({ name, digest: { hashAlgorithm: 'SHA-256', normalisationAlgorithm: 'jsonNormalisation/v4alpha1', value: 'x' }, signature: { algorithm, value: 'v', mediaType: 'm' } })

test('ocmRef prefixes http locations with oci::', () => {
  expect(ocmRef('oci::ghcr.io/acme', 'acme.org/app', '1.0.0')).toBe('oci::ghcr.io/acme//acme.org/app:1.0.0')
  expect(ocmRef('http://localhost:5000/ocm', 'acme.org/app', '1.0.0')).toBe('oci::http://localhost:5000/ocm//acme.org/app:1.0.0')
})

test('registryHost strips type and scheme and splits the port', () => {
  expect(registryHost('oci::ghcr.io/acme')).toEqual({ hostname: 'ghcr.io' })
  expect(registryHost('ghcr.io')).toEqual({ hostname: 'ghcr.io' })
  expect(registryHost('http://localhost:5000/ocm')).toEqual({ hostname: 'localhost', port: '5000' })
  expect(registryHost('oci::localhost:5000')).toEqual({ hostname: 'localhost', port: '5000' })
})

test('accessConfig names the registry host (and port)', () => {
  expect(accessConfig('oci::ghcr.io/acme')).toContain('hostname: ghcr.io\n')
  expect(accessConfig('oci::ghcr.io/acme')).not.toContain('port:')
  expect(accessConfig('http://localhost:5000/ocm')).toContain('hostname: localhost\n          port: "5000"\n')
  expect(accessConfig('oci::ghcr.io/acme')).toContain('type: OCIRepository')
})

test('shellQuote leaves safe words alone and single-quotes the rest', () => {
  expect(shellQuote('name=chart,arch=amd64')).toBe('name=chart,arch=amd64')
  expect(shellQuote('oci::ghcr.io/acme//acme.org/app:1.0.0+build.1')).toBe('oci::ghcr.io/acme//acme.org/app:1.0.0+build.1')
  expect(shellQuote('my file')).toBe("'my file'")
  expect(shellQuote("it's $HOME")).toBe("'it'\\''s $HOME'")
})

test('identityArg: name first, extra identity sorted', () => {
  expect(identityArg(res('image', 'ociImage', { os: 'linux', arch: 'amd64' }))).toBe('name=image,arch=amd64,os=linux')
  expect(identityArg(res('chart', 'helmChart'))).toBe('name=chart')
})

test('downloadCommand and transferCommand', () => {
  const ref = 'oci::ghcr.io/acme//acme.org/app:1.0.0'
  expect(downloadCommand(ref, res('image', 'ociImage', { arch: 'amd64' }))).toBe(`ocm download resource ${ref} --identity name=image,arch=amd64 --output image`)
  expect(downloadCommand(ref, res('my docs', 'documentation'))).toBe(`ocm download resource ${ref} --identity 'name=my docs' --output 'my docs'`)
  expect(transferCommand(ref)).toBe(`ocm transfer cv ${ref} <target-repository> --copy-resources --recursive`)
})

test('sortResources puts helm charts first, otherwise descriptor order', () => {
  const rs = [res('a', 'ociImage'), res('b', 'helmChart'), res('c', 'blob'), res('d', 'helmChart')]
  expect(sortResources(rs).map((r) => r.name)).toEqual(['b', 'd', 'a', 'c'])
})

test('verifyConfig: one consumer per signature; non-RSA gets a comment', () => {
  const y = verifyConfig([sig('default', 'RSASSA-PSS'), sig('prod', 'RSASSA-PKCS1-V1_5'), sig('cosign', 'sigstore')])
  expect(y).toContain('signature: default\n')
  expect(y).toContain('public_key_pem_file: /path/to/default.pub.pem')
  expect(y).toContain('algorithm: RSASSA-PKCS1-V1_5\n          signature: prod')
  expect(y).toContain('# signature algorithm sigstore: see the docs for its verifier')
  expect(y.match(/type: RSA\/v1alpha1/g)).toHaveLength(3)
})
