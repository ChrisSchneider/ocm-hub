import type { Component, VersionOverview } from '../api/client'
import { isAttestation, KINDS, levelAnchor, sameIdentity, type AttestationSet, type Entry, type Kind, type Level } from './attestations'
import { summaryItems } from './contentsSummary'
import { signatureLabel, sourceLink } from './facts'
import { collapseMinors } from './kubernetes'
import { sbomFormat, sbomResources } from './sbom'

export type RowKey = 'contents' | 'kubernetes' | 'architectures' | 'signature' | 'slsa' | 'sbom' | 'vex' | 'scans' | 'responsibles' | 'referencedBy'

// Target: where a row's details are shown (the view, and an anchor in it).
export type Target = { view?: 'contents' | 'versions' | 'descriptor' | 'attestations'; hash?: string }

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

// counts: a verification summary is evidence only when it claims a level
// (failed or unreadable summaries carry none).
const counts = (e: Entry) => isAttestation(e) && !(e.kind === 'slsa' && e.type?.startsWith('https://slsa.dev/verification_summary/') && !e.buildLevel)

// overviewRows: the Overview's items, one fixed row each. Descriptor rows
// come from the component and overview; the attestation rows from the set
// (undefined: loading, or setFailed when it could not be loaded).
export function overviewRows(component: Component, ov: VersionOverview, set: AttestationSet | undefined, setFailed: boolean): Row[] {
  const levels = set?.levels ?? []
  const absent = set && !setFailed && !levels.some((l) => l.status === 'failed') ? 'missing' : 'unknown'
  const of = (kinds: Kind[]) => {
    const pick = (l: Level) => l.entries.filter((e) => counts(e) && kinds.includes(e.kind))
    // The subject with the highest build level first: the row links to it.
    const best = (l: Level) => Math.max(0, ...pick(l).map((e) => e.buildLevel ?? 0))
    const found = levels.filter((l) => pick(l).length > 0).sort((a, b) => best(b) - best(a))
    return { entries: found.flatMap(pick), target: found[0] && ({ view: 'attestations', hash: levelAnchor(found[0].resource) } as Target) }
  }
  const list = (xs: string[], empty: string, sep = ', ', target?: Target): Partial<Row> =>
    xs.length > 0 ? { state: 'present', value: xs.join(sep), target } : { state: 'missing', value: empty }
  const fromAttestations = (kinds: Kind[], value: (entries: Entry[]) => string | undefined): Partial<Row> => {
    const { entries, target } = of(kinds)
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
    sbom: () => {
      const shipped = sbomResources(ov.resources)
      const embedded = (r: (typeof shipped)[number]) =>
        levels.flatMap((l) => l.entries).find((e) => e.how === 'embedded' && e.kind === 'sbom' && sameIdentity(e.resource, r))?.format
      const attested = of(['sbom'])
      const formats = [...shipped.map((r) => sbomFormat(r, embedded(r))), ...attested.entries.map((e) => e.format ?? '')]
      const families = unique(formats.map((f) => f.split(' ')[0])).sort()
      if (shipped.length === 0) return fromAttestations(['sbom'], () => families.join(' + ') || undefined)
      return { state: 'present', value: families.join(' + ') || undefined, target: attested.target ?? { view: 'contents' } }
    },
    vex: () =>
      fromAttestations(['vex'], (es) => {
        const best = es.find((e) => e.summary?.statements !== undefined) ?? es.find((e) => e.summary) ?? es[0]
        const statements = best.summary?.statements
        return [best.summary?.format ?? best.format, statements !== undefined ? `${statements} statements` : undefined].filter(Boolean).join(' · ') || undefined
      }),
    scans: () =>
      fromAttestations(SCANS, (es) => {
        const n = (k: Kind) => es.filter((e) => e.kind === k).length
        const kinds = SCANS.filter((k) => n(k) > 0).map((k) => (k === 'other' ? `${n(k)} other` : KINDS[k].label))
        const severe = es.filter((e) => e.summary?.importantBasis === 'security-severity')
        const high = severe.reduce((sum, e) => sum + (e.summary?.importantFindings ?? 0), 0)
        if (high > 0) kinds.push(`${high} high or critical`)
        return kinds.join(' · ')
      }),
  }
  return ROWS.map((r) => ({ ...r, ...body[r.key]() }) as Row)
}
