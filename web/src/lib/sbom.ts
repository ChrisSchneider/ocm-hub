// SBOMs shipped with a component: resources of type `sbom`, linked to the
// resource they describe by the `ocm.software/artifactReference` label
// (https://ocm.software/blog/2026-07-28-shipping-sboms-with-your-components/;
// a proof of concept, ocm-spec#146).

const ARTIFACT_REFERENCE = 'ocm.software/artifactReference'

// Resource is the part of an OCM resource the SBOM helpers read.
export interface Resource {
  name: string
  version?: string
  type?: string
  extraIdentity?: { [key: string]: string } | null
  labels?: readonly object[] | null
  access?: { [key: string]: unknown } | null
}

export const sbomResources = <R extends Resource>(resources: R[]): R[] => resources.filter((r) => r.type === 'sbom')

// artifactReference: the identity selector of the resource an SBOM describes.
export function artifactReference(r: Resource): Record<string, string> | undefined {
  const label = r.labels?.find((l) => (l as { name?: unknown }).name === ARTIFACT_REFERENCE) as { value?: unknown } | undefined
  const sel = (label?.value as { identitySelector?: unknown } | undefined)?.identitySelector
  if (!sel || typeof sel !== 'object' || typeof (sel as { name?: unknown }).name !== 'string') return undefined
  return Object.fromEntries(Object.entries(sel as Record<string, unknown>).filter(([, v]) => typeof v === 'string')) as Record<string, string>
}

// describedResource: the resource the SBOM's artifact reference selects (name
// plus every other selector key matched against the extra identity).
export function describedResource<R extends Resource>(sbom: Resource, resources: R[]): R | undefined {
  const sel = artifactReference(sbom)
  if (!sel) return undefined
  const { name, ...extra } = sel
  return resources.find((r) => r !== sbom && r.name === name && Object.entries(extra).every(([k, v]) => r.extraIdentity?.[k] === v))
}
