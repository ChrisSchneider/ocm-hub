import type { components } from '../api/schema'

type Schemas = components['schemas']
export type Entry = Schemas['AttestationEntry']
export type Level = Schemas['AttestationLevel']
export type Kind = Entry['kind']
export type ResourceIdentity = Schemas['ResourceIdentity']
export type AttestationSet = Schemas['AttestationSet']

// isHttpsUrl: only https URLs from documents are linked; anything else is shown as text.
export function isHttpsUrl(url: string): boolean {
  try {
    return new URL(url).protocol === 'https:'
  } catch {
    return false
  }
}

export const KINDS: Record<Kind, { label: string; explain: string }> = {
  slsa: { label: 'SLSA', explain: 'SLSA provenance — how and from which sources it was built' },
  sbom: { label: 'SBOM', explain: 'SBOM — list of the packages inside' },
  vex: { label: 'VEX', explain: "VEX — the vendor's statement on which known vulnerabilities actually affect it" },
  cve: { label: 'CVE', explain: 'CVE scan — known vulnerabilities a scanner found' },
  fips: { label: 'FIPS', explain: 'FIPS — uses FIPS 140 validated cryptography' },
  stig: { label: 'STIG', explain: 'STIG scan — checked against a DISA hardening guide' },
  secrets: { label: 'Secrets', explain: 'Secrets scan — checked for leaked keys and passwords' },
  virus: { label: 'Virus', explain: 'Virus scan — checked for malware' },
  sources: { label: 'Sources', explain: 'Image sources — where the source code and build materials are' },
  other: { label: 'Other', explain: 'Other attestation' },
}

export function howLabel(e: Entry): string {
  switch (e.how) {
    case 'embedded':
      return `Embedded resource ${e.resource?.name ?? ''}`.trim()
    case 'referrer':
      return 'OCI referrer'
    case 'cosignTag':
      return 'Cosign attestation tag'
    case 'buildx':
      return e.platform ? `Buildx attestation (${e.platform})` : 'Buildx attestation'
    case 'labelLink':
      return 'Label link'
  }
}

export function signedLabel(s: Entry['signed']): string {
  return s === 'signed' ? 'Signed (not verified)' : s === 'component' ? 'By component signature' : 'No'
}

export function sameIdentity(a?: ResourceIdentity, b?: ResourceIdentity): boolean {
  if (!a || !b) return !a && !b
  if (a.name !== b.name || (a.version ?? '') !== (b.version ?? '')) return false
  const x = Object.entries(a.extraIdentity ?? {})
  const y = b.extraIdentity ?? {}
  return x.length === Object.keys(y).length && x.every(([k, v]) => y[k] === v)
}

// isAttestation: an embedded SBOM is a resource shipped with the component,
// not an attestation; views list it separately.
export const isAttestation = (e: Entry): boolean => !(e.how === 'embedded' && e.kind === 'sbom')

// isOciAttestation: an attestation stored in the registry; a document linked from a
// label (e.g. VEX) is shown with the label instead.
export const isOciAttestation = (e: Entry): boolean => isAttestation(e) && e.how !== 'labelLink'

// EntryViewer: document is the parsed viewer (packages, VEX, scan results); raw the JSON.
export type EntryViewer = 'document' | 'raw'

// DocumentPart: a part of a parsed document; one document may carry several.
export type DocumentPart = 'sbom' | 'vex' | 'scan'

// attestationName: the row title of an attestation.
export function attestationName(e: Entry): string {
  if (e.kind === 'slsa') return e.type?.startsWith('https://slsa.dev/verification_summary/') ? 'SLSA verification summary' : 'SLSA provenance'
  return KINDS[e.kind].explain.split(' — ')[0]
}

// detailsText: the one-line facts of an attestation, missing parts skipped. A document
// counts what it carries, whatever its kind: a CycloneDX SBOM may hold VEX and findings too.
export function detailsText(e: Entry): string {
  const s = e.summary
  const parts: (string | undefined)[] = []
  if (e.kind === 'slsa') {
    if (e.buildLevel !== undefined) parts.push(`Build L${e.buildLevel}`)
    parts.push(e.builder)
    if (!parts.some(Boolean)) parts.push(e.format)
  } else if (s) {
    parts.push(s.tool, s.format)
    if (s.packages !== undefined) parts.push(`${s.packages} packages`)
    if (s.statements !== undefined) parts.push(`${s.statements} statements`)
    if (s.affected !== undefined) parts.push(`${s.affected} affected`)
    if (s.findings !== undefined) parts.push(`${s.findings} findings`)
    if (s.importantFindings !== undefined)
      parts.push(s.importantBasis === 'security-severity' ? `${s.importantFindings} high or critical` : s.importantBasis === 'level' ? `${s.importantFindings} with level error` : undefined)
  } else parts.push(e.format)
  return parts.filter(Boolean).join(' · ')
}

const parsedFormats = ['SPDX', 'CycloneDX', 'OpenVEX', 'SARIF']

// entryViewer: which in-page viewer can open the attestation document. A summary
// means the sync parsed it; without one the format decides.
export function entryViewer(e: Entry): EntryViewer | undefined {
  if (!e.digest || e.how === 'embedded') return undefined
  if (e.kind === 'slsa') return 'raw'
  if (e.summary) return 'document'
  const f = e.format ?? ''
  return parsedFormats.some((p) => f.startsWith(p)) ? 'document' : undefined
}

// entryPart: the document part an attestation is opened at.
export const entryPart = (e: Entry): DocumentPart => (e.kind === 'vex' ? 'vex' : e.kind === 'sbom' ? 'sbom' : 'scan')

// levelFor: the level of a resource, or of the component when id is undefined.
export function levelFor(set: AttestationSet | undefined, id?: ResourceIdentity): Level | undefined {
  return set?.levels.find((l) => sameIdentity(l.resource, id))
}

// entriesOf: OCI attestations of a level; without platform only the platform-less ones.
export function entriesOf(level: Level | undefined, platform?: string): Entry[] {
  return (level?.entries ?? []).filter((e) => isOciAttestation(e) && (platform === undefined ? !e.platform : e.platform === platform))
}

export function labelEntry(set: AttestationSet | undefined, id: ResourceIdentity | undefined, label: string): Entry | undefined {
  return levelFor(set, id)?.entries.find((e) => e.how === 'labelLink' && e.label === label)
}

// levelAnchor is the element id of a level's table, so other views can link to it.
export function levelAnchor(id?: ResourceIdentity): string {
  if (!id) return 'attestations-component'
  const extra = Object.keys(id.extraIdentity ?? {})
    .sort()
    .map((k) => `-${k}=${id.extraIdentity![k]}`)
    .join('')
  return `attestations-${id.name}${id.version ? `-${id.version}` : ''}${extra}`.replace(/[^A-Za-z0-9._=-]/g, '_')
}
