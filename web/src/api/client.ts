import createClient from 'openapi-fetch'
import type { OcmDescriptor } from './ocm'
import type { components, paths } from './schema'

export type ComponentSummary = components['schemas']['ComponentSummary']
export type ComponentList = components['schemas']['ComponentList']
export type Component = components['schemas']['Component']
export type Source = components['schemas']['Source']
export type ConnectorFacet = components['schemas']['ConnectorFacet']
export type GroupFacet = components['schemas']['GroupFacet']
export type SourceGroup = components['schemas']['SourceGroup']
export type VersionOverview = components['schemas']['ComponentVersionOverview']
export type OcmResource = VersionOverview['resources'][number]
export type OcmSignature = VersionOverview['signatures'][number]
export type VersionEntry = components['schemas']['VersionEntry']
export interface LocatedDescriptor {
  location: string
  descriptor: OcmDescriptor
}
type ErrorResponse = components['schemas']['ErrorResponse']

// fetch is resolved per request: openapi-fetch otherwise captures it at creation,
// before test servers (MSW) patch globalThis.fetch.
export const api = createClient<paths>({
  baseUrl: globalThis.location?.origin ?? '',
  fetch: (request) => globalThis.fetch(request),
})

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly detail: Record<string, unknown> = {},
  ) {
    super(message)
  }
}

function fail(status: number, error: unknown): never {
  const item = (error as ErrorResponse | undefined)?.errors?.[0]
  throw new ApiError(status, item?.code ?? 'UNKNOWN', item?.message ?? `request failed with status ${status}`, item?.detail)
}

export async function listComponents(p: { q?: string; connector?: string[]; category?: string[]; architecture?: string[]; provider?: string; topLevel?: boolean; replication?: 'single' | 'replicated'; includePrerelease?: boolean; sort?: 'relevance' | 'updated' | 'name'; cursor?: string; limit?: number }): Promise<ComponentList> {
  const { data, error, response } = await api.GET('/v1/components', { params: { query: p } })
  if (!data) fail(response.status, error)
  return data
}

export async function getComponent(name: string): Promise<Component> {
  const { data, error, response } = await api.GET('/v1/components/{name}', { params: { path: { name } } })
  if (!data) fail(response.status, error)
  return data
}

export async function getVersions(name: string, p: { semverConstraint?: string; includePrerelease?: boolean; cursor?: string } = {}) {
  const { data, error, response } = await api.GET('/v1/components/{name}/versions', { params: { path: { name }, query: p } })
  if (!data) fail(response.status, error)
  return data
}

export async function getOverview(name: string, version: string, repository?: string): Promise<VersionOverview> {
  const { data, error, response } = await api.GET('/v1/components/{name}/overview/{version}', {
    params: { path: { name, version }, query: repository ? { repository } : {} },
  })
  if (!data) fail(response.status, error)
  return data
}

export type AttestationSet = components['schemas']['AttestationSet']

// getAttestations throws ApiError NOT_COLLECTED (404) for versions and locations the sync did not gather.
export async function getAttestations(name: string, version: string, repository?: string): Promise<AttestationSet> {
  const { data, error, response } = await api.GET('/v1/components/{name}/attestations/{version}', {
    params: { path: { name, version }, query: repository ? { repository } : {} },
  })
  if (!data) fail(response.status, error)
  return data
}

export async function getDescriptor(name: string, version: string, repository?: string): Promise<OcmDescriptor> {
  const { data, error, response } = await api.GET('/v1/components/{name}/descriptors/{version}', {
    params: { path: { name, version }, query: repository ? { repository } : {} },
    headers: { Accept: 'application/json' },
  })
  if (!data) fail(response.status, error)
  return data as unknown as OcmDescriptor
}

export async function getDescriptorTree(name: string, version: string, repository?: string, opts: { compact?: boolean } = {}): Promise<LocatedDescriptor[]> {
  const { data, error, response } = await api.GET('/v1/components/{name}/descriptors/{version}', {
    params: { path: { name, version }, query: { recursive: -1, compact: opts.compact ?? true, ...(repository ? { repository } : {}) } },
    headers: { Accept: 'application/json' },
  })
  if (!data) fail(response.status, error)
  if (!Array.isArray(data)) throw new ApiError(response.status, 'UNEXPECTED', 'expected a list of located descriptors')
  return data as unknown as LocatedDescriptor[]
}

export async function getDescriptorYAML(name: string, version: string, repository?: string): Promise<string> {
  const { data, error, response } = await api.GET('/v1/components/{name}/descriptors/{version}', {
    params: { path: { name, version }, query: repository ? { repository } : {} },
    headers: { Accept: 'application/yaml' },
    parseAs: 'text',
  })
  if (data === undefined) fail(response.status, error)
  return data as unknown as string
}

// componentUrl: a /v1/components/{name}/… path with each segment encoded and
// extraIdentity.<key>, repository and then `extra` as query parameters.
function componentUrl(name: string, segments: string[], extraIdentity?: Record<string, string> | null, repository?: string, extra: Record<string, string> = {}): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(extraIdentity ?? {})) q.set(`extraIdentity.${k}`, v)
  if (repository) q.set('repository', repository)
  for (const [k, v] of Object.entries(extra)) q.set(k, v)
  const query = q.toString()
  return `/v1/components/${[name, ...segments].map(encodeURIComponent).join('/')}${query ? `?${query}` : ''}`
}

export function resourceUrl(name: string, version: string, resource: string, extraIdentity?: Record<string, string> | null, repository?: string): string {
  return componentUrl(name, ['resources', version, resource], extraIdentity, repository)
}

export type DocumentSource = { attestation: string } | { resource: string; extraIdentity?: Record<string, string> | null }
export type HubDocument = components['schemas']['Document']
export type ImageIndex = components['schemas']['ImageIndex']
export type ImageDetails = components['schemas']['ImageDetails']

export function attestationContentUrl(name: string, version: string, digest: string, repository?: string, asDocument?: boolean): string {
  return componentUrl(name, ['attestations', version, digest], null, repository, asDocument ? { as: 'document' } : {})
}

// getJson: these routes take extraIdentity.<key> parameters, which the typed client cannot express.
async function getJson<T>(url: string): Promise<T> {
  const response = await globalThis.fetch(new URL(url, globalThis.location?.origin), { headers: { Accept: 'application/json' } })
  const body: unknown = await response.json().catch(() => undefined)
  if (!response.ok) fail(response.status, body)
  return body as T
}

// getDocument throws ApiError UNSUPPORTED_DOCUMENT, DOCUMENT_TOO_LARGE (422), DOCUMENT_CHANGED (409, detail.url)
// and DOCUMENT_NOT_ALLOWED (403, detail.url).
export async function getDocument(name: string, version: string, src: DocumentSource, repository?: string): Promise<HubDocument> {
  const url = 'attestation' in src
    ? attestationContentUrl(name, version, src.attestation, repository, true)
    : componentUrl(name, ['resources', version, src.resource], src.extraIdentity, repository, { as: 'document' })
  return getJson<HubDocument>(url)
}

// getAttestationJson: the attestation document as stored (the predicate of a statement), parsed as plain JSON.
export async function getAttestationJson(name: string, version: string, digest: string, repository?: string): Promise<unknown> {
  return getJson<unknown>(attestationContentUrl(name, version, digest, repository))
}

export async function getImage(name: string, version: string, resource: string, extraIdentity?: Record<string, string> | null, repository?: string): Promise<ImageIndex> {
  return getJson<ImageIndex>(componentUrl(name, ['images', version, resource], extraIdentity, repository))
}

export async function getImageDetails(name: string, version: string, resource: string, extraIdentity: Record<string, string> | null | undefined, digest: string, repository?: string): Promise<ImageDetails> {
  return getJson<ImageDetails>(componentUrl(name, ['images', version, resource, digest], extraIdentity, repository))
}

export function imageRawUrl(name: string, version: string, resource: string, extraIdentity: Record<string, string> | null | undefined, digest: string, part: 'manifest' | 'config', repository?: string): string {
  return componentUrl(name, ['images', version, resource, digest], extraIdentity, repository, { raw: part })
}

// displayLocation drops the "oci::" prefix for display.
export const displayLocation = (l: string) => l.replace(/^oci::/, '')
