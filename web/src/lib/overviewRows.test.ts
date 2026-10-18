import { expect, test } from 'vitest'
import type { Component, VersionOverview } from '../api/client'
import type { AttestationSet, Entry } from './attestations'
import { overviewRows, type Row } from './overviewRows'

const comp = (o: Partial<Component> = {}): Component => ({
  name: 'acme.org/x', title: 'x', versionCount: 1, signed: false, defaultVersion: '1.0', referencedBy: [], referencedByCount: 0, links: { versions: '' }, sources: [],
  locations: [{ repository: 'oci::ghcr.io/acme', latest: '1.0', versionCount: 1 }],
  ...o,
})
const ov = (o: Partial<VersionOverview> = {}): VersionOverview => ({
  name: 'acme.org/x', version: '1.0', repository: 'oci::ghcr.io/acme', title: 'x', signed: false, responsibles: [], docs: [],
  contents: { resources: [], references: 0 }, architectures: [], imagePlatformsRead: true, resources: [], references: [], sources: [], signatures: [],
  ...o,
})
const img = { name: 'image', version: '1.0', extraIdentity: { arch: 'amd64' } }
const e = (o: Partial<Entry>): Entry => ({ kind: 'other', how: 'referrer', signed: 'no', ...o })
const set = (component: Entry[], image: Entry[] = []): AttestationSet => ({
  name: 'acme.org/x', version: '1.0', repository: 'oci::ghcr.io/acme', checkedAt: '2026-01-01T00:00:00Z',
  levels: [
    { status: 'ok', entries: component },
    { resource: img, status: 'ok', entries: image },
  ],
})
const row = (rows: Row[], key: string) => rows.find((r) => r.key === key)!

test('every row is present in order even when empty', () => {
  const rows = overviewRows(comp(), ov(), set([]), false)
  expect(rows.map((r) => r.key)).toEqual(['contents', 'kubernetes', 'architectures', 'signature', 'slsa', 'sbom', 'vex', 'scans', 'responsibles', 'referencedBy'])
  expect(rows.map((r) => r.label)).toEqual(['Contents', 'Compatibility', 'Architectures', 'OCM signature', 'Provenance', 'SBOM', 'VEX', 'Scans', 'Responsibles', 'Used by'])
  expect(rows.map((r) => r.section)).toEqual(['facts', 'facts', 'facts', ...Array(5).fill('evidence'), 'facts', 'facts'])
  expect(row(rows, 'responsibles')).toMatchObject({ state: 'missing', value: 'Not declared' })
  expect(row(rows, 'kubernetes')).toMatchObject({ state: 'missing', value: 'Not declared' })
  expect(row(rows, 'signature')).toMatchObject({ state: 'missing', value: 'Not signed' })
  expect(rows.every((r) => r.state === 'missing' && !r.target)).toBe(true)
  // Only the evidence explains itself.
  expect(rows.filter((r) => r.why).map((r) => r.key)).toEqual(['signature', 'slsa', 'sbom', 'vex', 'scans'])
  expect(row(rows, 'vex')).toMatchObject({ state: 'missing', value: 'Not attached' })
})

test('signature row shows name and algorithm', () => {
  const sig = { name: 'portal.platform-mesh', digest: { hashAlgorithm: 'SHA-256', normalisationAlgorithm: 'jsonNormalisation/v4alpha1', value: 'abc' }, signature: { algorithm: 'RSASSA-PSS', mediaType: 'application/x-pem-file', value: 'x' } }
  expect(row(overviewRows(comp(), ov({ signatures: [sig] }), undefined, false), 'signature')).toMatchObject({
    state: 'present', value: 'portal.platform-mesh · RSASSA-PSS · certificate', target: { view: 'descriptor' },
  })
})

test('slsa row merges build level and source', () => {
  const sources = [{ name: 'src', version: '1', type: 'git', access: { type: 'gitHub', repoUrl: 'https://github.com/acme/web', commit: '0123456789' } }]
  const both = row(overviewRows(comp(), ov({ sources }), set([e({ kind: 'slsa', format: 'SLSA v1' })], [e({ kind: 'slsa', buildLevel: 3 })]), false), 'slsa')
  expect(both).toMatchObject({ state: 'present', value: 'Build Level 3 · github.com/acme/web @ 0123456', target: { view: 'attestations', hash: 'attestations-image-1.0-arch=amd64' } })
  // The source alone is present without the attestation set, and has nowhere to go.
  const src = row(overviewRows(comp(), ov({ sources }), undefined, true), 'slsa')
  expect(src).toMatchObject({ state: 'present', value: 'github.com/acme/web @ 0123456' })
  expect(src.target).toBeUndefined()
  expect(row(overviewRows(comp(), ov(), set([e({ kind: 'slsa', format: 'SLSA v1' }), e({ kind: 'slsa', format: 'SLSA v0.2' })]), false), 'slsa').value).toBe('SLSA v1 + SLSA v0.2')
})

test('sbom row counts shipped and attested documents', () => {
  const sbom = { name: 'sbom', version: '1', type: 'sbom', relation: 'local' as const, access: { mediaType: 'application/vnd.cyclonedx+json' } }
  const both = row(overviewRows(comp(), ov({ resources: [sbom] }), set([], [e({ kind: 'sbom', format: 'SPDX' })]), false), 'sbom')
  expect(both).toMatchObject({ state: 'present', value: '2 documents', target: { view: 'attestations', hash: 'attestations-image-1.0-arch=amd64' } })
  // Only shipped: present without the set, and points to Contents.
  expect(row(overviewRows(comp(), ov({ resources: [sbom] }), undefined, true), 'sbom')).toMatchObject({ state: 'present', value: '1 document', target: { view: 'contents' } })
  // A VEX document listing packages counts as well.
  const vex = e({ kind: 'vex', how: 'labelLink', summary: { format: 'CycloneDX 1.6', packages: 14, statements: 56 } })
  expect(row(overviewRows(comp(), ov(), set([vex]), false), 'sbom')).toMatchObject({ state: 'present', value: '1 document', target: { view: 'labels' } })
})

test('vex row counts documents', () => {
  const r = row(overviewRows(comp(), ov(), set([e({ kind: 'vex', how: 'labelLink', format: 'OpenVEX' }), e({ kind: 'vex', format: 'OpenVEX', summary: { format: 'OpenVEX v0.2.0', statements: 12 } })]), false), 'vex')
  expect(r).toMatchObject({ state: 'present', value: '2 documents', target: { view: 'attestations', hash: 'attestations-component' } })
})

test('vex row: a label-only VEX links to Labels; VEX inside an SBOM counts', () => {
  const linked = e({ kind: 'vex', how: 'labelLink', summary: { format: 'CycloneDX 1.6', statements: 56 } })
  expect(row(overviewRows(comp(), ov(), set([linked]), false), 'vex')).toMatchObject({ state: 'present', value: '1 document', target: { view: 'labels' } })
  const sbom = e({ kind: 'sbom', summary: { format: 'CycloneDX 1.6', packages: 3, statements: 2, findings: 1 } })
  expect(row(overviewRows(comp(), ov(), set([], [sbom]), false), 'vex')).toMatchObject({ state: 'present', value: '1 document', target: { view: 'attestations' } })
  expect(row(overviewRows(comp(), ov(), set([], [sbom]), false), 'scans')).toMatchObject({ state: 'present', value: 'CVE' })
})

test('scans row sums high or critical', () => {
  const sarif = (n: number, basis = 'security-severity') => e({ kind: 'cve', summary: { format: 'SARIF 2.1.0', importantBasis: basis, importantFindings: n } })
  const r = row(overviewRows(comp(), ov(), set([e({ kind: 'secrets' }), e({ kind: 'other' })], [sarif(3), sarif(1), sarif(5, 'level'), e({ kind: 'other' }), e({ kind: 'fips' })]), false), 'scans')
  expect(r).toMatchObject({ state: 'present', value: 'CVE · Secrets · FIPS · 2 other · 4 high or critical' })
  expect(r.target?.view).toBe('attestations')
  // A sum of 0 says nothing.
  expect(row(overviewRows(comp(), ov(), set([], [sarif(0)]), false), 'scans')).toMatchObject({ state: 'present', value: 'CVE' })
  // Sources and SLSA attestations are not scans.
  expect(row(overviewRows(comp(), ov(), set([e({ kind: 'sources' }), e({ kind: 'slsa' })]), false), 'scans').state).toBe('missing')
})

test('attestation rows are Not checked when the set failed', () => {
  for (const rows of [overviewRows(comp(), ov(), undefined, true), overviewRows(comp(), ov(), undefined, false)]) {
    for (const k of ['slsa', 'sbom', 'vex', 'scans']) expect(row(rows, k)).toMatchObject({ state: 'unknown' })
    expect(row(rows, 'kubernetes').state).toBe('missing')
  }
  const s = set([e({ kind: 'vex' })])
  s.levels[1] = { resource: img, status: 'failed', error: '401', entries: [] }
  const rows = overviewRows(comp(), ov(), s, false)
  expect(row(rows, 'vex').state).toBe('present')
  expect(row(rows, 'slsa').state).toBe('unknown')
})

test('descriptor rows: contents, kubernetes, architectures', () => {
  const rows = overviewRows(
    comp(),
    ov({
      contents: { resources: [{ type: 'changelog', count: 1 }, { type: 'ociImage', count: 2 }], references: 0 },
      kubernetes: { constraint: '>=1.28', versions: ['1.28', '1.29', '1.31'], distributions: ['gardener', 'eks'] },
      architectures: [{ platform: 'linux/amd64', from: ['image', 'ocm'] }, { platform: 'linux/arm64', from: ['image'] }],
    }),
    undefined,
    false,
  )
  expect(row(rows, 'contents')).toMatchObject({ state: 'present', value: '2 OCI images · 1 changelog', target: { view: 'contents' } })
  expect(row(rows, 'kubernetes').value).toBe('1.28–1.29, 1.31 · gardener · eks')
  // storage alone is enough
  expect(row(overviewRows(comp(), ov({ storage: [{ accessMode: 'RWX', volumeMode: 'Block' }] }), undefined, false), 'kubernetes')).toMatchObject({ state: 'present', value: 'RWX block' })
  expect(row(rows, 'architectures')).toMatchObject({ state: 'present', value: 'linux/amd64, linux/arm64', target: { view: 'contents' } })
})
