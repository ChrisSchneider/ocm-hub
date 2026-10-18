import type { Component, VersionOverview } from '../api/client'
import { embeddedLevel, isAttestation, KINDS, levelAnchor, sameIdentity, type SupplyChain, type Entry, type Kind, type Level } from './supplyChain'
import { summaryItems } from './contentsSummary'
import { plural, signatureLabel, sourceLink } from './facts'
import { collapseMinors } from './kubernetes'
import { sbomFormat, sbomResources } from './sbom'

export type RowKey = 'contents' | 'kubernetes' | 'architectures' | 'signature' | 'slsa' | 'sbom' | 'vex' | 'scans' | 'responsibles' | 'referencedBy'

// Target: where a row's details are shown (the view, and an anchor in it).
export type Target = { view?: 'contents' | 'versions' | 'descriptor' | 'labels' | 'supply-chain'; hash?: string }

export interface Row {
  key: RowKey
  // evidence: the supply-chain list; facts: shown in the pods and the sidebar.
  section: 'evidence' | 'facts'
  label: string
  // why: what the evidence is for (evidence rows only).
  why?: string
  // present: shown with value; missing: value is the empty text ("None");
  // unknown: depends on attestations that are not loaded or a subject whose check failed.
  state: 'present' | 'missing' | 'unknown'
  value?: string
  target?: Target
}

const ROWS: { key: RowKey; section: Row['section']; label: string; why?: string }[] = [
  { key: 'contents', section: 'facts', label: 'Contents' },
  { key: 'kubernetes', section: 'facts', label: 'Compatibility' },
  { key: 'architectures', section: 'facts', label: 'Architectures' },
  { key: 'signature', section: 'evidence', label: 'OCM signature', why: "Lets consumers verify that the component version hasn't been tampered with since it was signed." },
  {
    key: 'slsa',
    section: 'evidence',
    label: 'Provenance',
    why: 'Records how and from which sources it was built (SLSA). Higher build levels make the build harder to tamper with. Points to the source code for audits and rebuilds.',
  },
  { key: 'sbom', section: 'evidence', label: 'SBOM', why: 'Lists the packages inside, so you can track vulnerabilities and licenses.' },
  { key: 'vex', section: 'evidence', label: 'VEX', why: "The vendor's statement on which known vulnerabilities actually affect it, so scanner findings can be triaged." },
  { key: 'scans', section: 'evidence', label: 'Scans', why: 'Shows which scans (vulnerabilities, secrets, malware, hardening) were run and attached.' },
  { key: 'responsibles', section: 'facts', label: 'Responsibles' },
  { key: 'referencedBy', section: 'facts', label: 'Used by' },
]

// storageLabel: a volume need such as "RWX filesystem".
export const storageLabel = (n: NonNullable<VersionOverview['storage']>[number]) => `${n.accessMode} ${n.volumeMode.toLowerCase()}`

const SCANS: Kind[] = ['cve', 'secrets', 'virus', 'stig', 'fips', 'other']

const unique = (xs: string[]) => [...new Set(xs.filter(Boolean))]

// documents: "2 documents · CycloneDX + SPDX", the format families sorted.
const documents = (formats: string[]) =>
  [plural(formats.length, 'document', 'documents'), unique(formats.map((f) => f.split(' ')[0])).sort().join(' + ')].filter(Boolean).join(' · ')

const entryFormat = (e: Entry) => e.summary?.format ?? e.format ?? ''

// counts: a verification summary is evidence only when it claims a level
// (failed or unreadable summaries carry none).
const counts = (e: Entry) => isAttestation(e) && !(e.kind === 'slsa' && e.type?.startsWith('https://slsa.dev/verification_summary/') && !e.buildLevel)

// overviewRows: the Overview's items, one fixed row each. Descriptor rows
// come from the component and overview; the attestation rows from the set
// (undefined: loading, or setFailed when it could not be loaded).
export function overviewRows(component: Component, ov: VersionOverview, set: SupplyChain | undefined, setFailed: boolean): Row[] {
  const levels = set?.levels ?? []
  const absent = set && !setFailed && !levels.some((l) => l.status === 'failed') ? 'missing' : 'unknown'
  // of: the entries of kinds, or carrying what also asks for (a CycloneDX SBOM may hold VEX
  // and findings). The row links to the subject in the Supply chain view.
  const of = (kinds: Kind[], also: (e: Entry) => boolean = () => false) => {
    const pick = (l: Level) => l.entries.filter((e) => counts(e) && (kinds.includes(e.kind) || also(e)))
    // The subject with the highest build level first: the row links to it.
    const best = (l: Level) => Math.max(0, ...pick(l).map((e) => e.buildLevel ?? 0))
    const found = levels.filter((l) => pick(l).length > 0).sort((a, b) => best(b) - best(a))
    const target: Target | undefined = found[0] && { view: 'supply-chain', hash: levelAnchor(found[0].resource) }
    return { entries: found.flatMap(pick), target }
  }
  const list = (xs: string[], empty: string, sep = ', ', target?: Target): Partial<Row> =>
    xs.length > 0 ? { state: 'present', value: xs.join(sep), target } : { state: 'missing', value: empty }
  const fromAttestations = (kinds: Kind[], value: (entries: Entry[]) => string | undefined, also?: (e: Entry) => boolean): Partial<Row> => {
    const { entries, target } = of(kinds, also)
    if (entries.length > 0) return { state: 'present', value: value(entries), target }
    return absent === 'missing' ? { state: 'missing', value: 'Not attached' } : { state: 'unknown' }
  }

  const body: Record<RowKey, () => Partial<Row>> = {
    responsibles: () =>
      list(
        ov.responsibles.map((r) => (r.type === 'githubUser' ? `@${r.username ?? ''}` : r.type === 'githubTeam' ? (r.teamname ?? '') : 'CODEOWNERS')),
        'Not declared',
      ),
    referencedBy: () => list(component.referencedBy.map((p) => p.title || p.name), 'None'),
    contents: () => list(summaryItems(ov.contents), 'None', ' · ', { view: 'contents' }),
    kubernetes: () => {
      const k = ov.kubernetes
      return list([...(k ? [collapseMinors(k.versions) || k.constraint, ...k.distributions] : []), ...(ov.storage ?? []).map(storageLabel)], 'Not declared', ' · ')
    },
    architectures: () => list((ov.architectures ?? []).map((a) => a.platform), 'Not declared', ', ', { view: 'contents' }),
    signature: () => list(ov.signatures.map((s) => `${s.name} · ${signatureLabel([s])}`), 'Not signed', ', ', { view: 'descriptor' }),
    slsa: () => {
      const src = sourceLink(ov.sources ?? [])
      const att = fromAttestations(['slsa'], (es) => {
        const level = Math.max(0, ...es.map((e) => e.buildLevel ?? 0))
        return level > 0 ? `Build Level ${level}` : unique(es.map((e) => e.format ?? '')).join(' + ') || undefined
      })
      if (!src) return att
      const value = [att.state === 'present' ? att.value : undefined, src.label].filter(Boolean).join(' · ')
      return { state: 'present', value, target: att.target }
    },
    // sbom and vex: the documents carrying packages or statements and their formats; one
    // document (a CycloneDX VEX listing its components) may count for both.
    sbom: () => {
      const shipped = sbomResources(ov.resources)
      const embedded = (r: (typeof shipped)[number]) =>
        levels.flatMap((l) => l.entries).find((e) => e.source === 'embedded' && e.kind === 'sbom' && sameIdentity(e.resource, r))?.format
      const withPackages = (e: Entry) => !!e.summary?.packages
      const attested = of(['sbom'], withPackages)
      if (shipped.length === 0) return fromAttestations(['sbom'], (es) => documents(es.map(entryFormat)), withPackages)
      const formats = [...shipped.map((r) => sbomFormat(r, embedded(r))), ...attested.entries.map(entryFormat)]
      const at = embeddedLevel(set, shipped[0])
      return { state: 'present', value: documents(formats), target: attested.target ?? (at ? { view: 'supply-chain', hash: levelAnchor(at.resource) } : { view: 'contents' }) }
    },
    vex: () => fromAttestations(['vex'], (es) => documents(es.map(entryFormat)), (e) => !!e.summary?.statements),
    scans: () =>
      fromAttestations(SCANS, (es) => {
        // Findings in another kind's document (a CycloneDX SBOM) count as a CVE scan.
        const n = (k: Kind) => es.filter((e) => e.kind === k || (k === 'cve' && !SCANS.includes(e.kind))).length
        const kinds = SCANS.filter((k) => n(k) > 0).map((k) => (k === 'other' ? `${n(k)} other` : KINDS[k].label))
        const severe = es.filter((e) => e.summary?.importantBasis === 'security-severity')
        const high = severe.reduce((sum, e) => sum + (e.summary?.importantFindings ?? 0), 0)
        if (high > 0) kinds.push(`${high} high or critical`)
        return kinds.join(' · ')
      }, (e) => !!e.summary?.findings),
  }
  return ROWS.map((r) => ({ ...r, ...body[r.key]() }) as Row)
}
