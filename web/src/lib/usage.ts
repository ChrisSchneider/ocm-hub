import type { OcmResource, OcmSignature } from '../api/client'

// Registry host of a location: type prefix (oci::) and scheme stripped,
// up to the first '/'; a port is split off as OCM consumer identities
// carry it separately.
export function registryHost(repository: string): { hostname: string; port?: string } {
  const host = repository.replace(/^[A-Za-z]+::/, '').replace(/^https?:\/\//, '').split('/')[0]
  const i = host.lastIndexOf(':')
  return i < 0 ? { hostname: host } : { hostname: host.slice(0, i), port: host.slice(i + 1) }
}

// One shell word: unchanged when it only has safe characters, else single-quoted.
export function shellQuote(s: string): string {
  return /^[A-Za-z0-9._/:@=,+-]+$/.test(s) ? s : `'${s.replace(/'/g, `'\\''`)}'`
}

export function identityArg(r: OcmResource): string {
  const extra = Object.entries(r.extraIdentity ?? {}).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
  return [`name=${r.name}`, ...extra.map(([k, v]) => `${k}=${v}`)].join(',')
}

export const downloadCommand = (ref: string, r: OcmResource): string =>
  `ocm download resource ${shellQuote(ref)} --identity ${shellQuote(identityArg(r))} --output ${shellQuote(r.name)}`

export const transferCommand = (ref: string): string => `ocm transfer cv ${shellQuote(ref)} <target-repository> --copy-resources --recursive`

// ctfArchive: a local CTF archive path named after the component and version ("./app-2.0.0.ctf").
export const ctfArchive = (name: string, version: string): string => `./${name.split('/').pop()}-${version}.ctf`

// ctfDownloadCommand copies the component version (resources and references) into a local CTF archive.
export const ctfDownloadCommand = (ref: string, archive: string): string =>
  `ocm transfer cv ${shellQuote(ref)} ${shellQuote(`ctf::${archive}`)} --copy-resources --recursive`

// ctfUploadCommand copies it from the archive into a target registry.
export const ctfUploadCommand = (archive: string, name: string, version: string): string =>
  `ocm transfer cv ${shellQuote(`ctf::${archive}//${name}:${version}`)} <target-repository> --copy-resources --recursive`

// Helm charts first, otherwise descriptor order (stable sort).
export const sortResources = (rs: OcmResource[]): OcmResource[] => [...rs].sort((a, b) => Number(b.type === 'helmChart') - Number(a.type === 'helmChart'))

const HEADER = 'type: generic.config.ocm.software/v1\nconfigurations:\n  - type: credentials.config.ocm.software\n    consumers:\n'

export function accessConfig(repository: string): string {
  const { hostname, port } = registryHost(repository)
  return (
    HEADER +
    '      - identity:\n' +
    '          type: OCIRepository\n' +
    `          hostname: ${hostname}\n` +
    (port ? `          port: "${port}"\n` : '') +
    '        credentials:\n' +
    '          - type: Credentials/v1\n' +
    '            properties:\n' +
    '              username: <username>\n' +
    '              password: <token>\n'
  )
}

const RSA = new Set(['RSASSA-PSS', 'RSASSA-PKCS1-V1_5'])

// The CLI resolves verification keys per signature name (exact match), so
// every signature gets its own consumer.
export function verifyConfig(signatures: OcmSignature[]): string {
  return (
    HEADER +
    signatures
      .map((s) => {
        const rsa = RSA.has(s.signature.algorithm)
        return (
          (rsa ? '' : `      # signature algorithm ${s.signature.algorithm}: see the docs for its verifier\n`) +
          '      - identity:\n' +
          '          type: RSA/v1alpha1\n' +
          `          algorithm: ${rsa ? s.signature.algorithm : 'RSASSA-PSS'}\n` +
          `          signature: ${s.name}\n` +
          '        credentials:\n' +
          '          - type: Credentials/v1\n' +
          '            properties:\n' +
          `              public_key_pem_file: /path/to/${s.name}.pub.pem\n`
        )
      })
      .join('')
  )
}
