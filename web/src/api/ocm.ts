// The parts of an OCM v2 component descriptor the UI reads. The API types a
// descriptor as a free-form object (api/openapi.yaml), so this is our view.
export interface OcmLabel {
  name: string
  value: unknown
}

export interface OcmElement {
  name: string
  version: string
  type?: string
  relation?: string
  extraIdentity?: Record<string, string> | null
  labels?: OcmLabel[] | null
  access?: Record<string, unknown> | null
  digest?: unknown
}

export interface OcmReference {
  name: string
  componentName: string
  version: string
  extraIdentity?: Record<string, string> | null
  digest?: unknown
}

export interface OcmDescriptor {
  meta: { schemaVersion: string }
  component: {
    name: string
    version: string
    provider: string | { name: string }
    creationTime?: string
    labels?: OcmLabel[] | null
    resources?: OcmElement[] | null
    sources?: OcmElement[] | null
    componentReferences?: OcmReference[] | null
  }
}

export const providerName = (d: OcmDescriptor): string =>
  typeof d.component.provider === 'string' ? d.component.provider : (d.component.provider?.name ?? '')

export const descId = (name: string, version: string): string => `${name}:${version}`

// Component names never contain ':', so the first ':' separates the version.
export function parseId(id: string): { name: string; version: string } {
  const i = id.indexOf(':')
  return i < 0 ? { name: id, version: '' } : { name: id.slice(0, i), version: id.slice(i + 1) }
}

// ocm CLI component reference: <repository>//<name>:<version>; http://
// locations need the explicit oci:: type.
export function ocmRef(repository: string, name: string, version: string): string {
  const repo = repository.startsWith('http://') ? `oci::${repository}` : repository
  return `${repo}//${name}:${version}`
}
