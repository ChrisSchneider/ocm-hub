import { attestationName, detailsText, entriesOf, entryViewer, howLabel, labelEntry, levelAnchor, levelFor, sameIdentity, signedLabel, type AttestationSet, type Entry } from './attestations'

const e = (kind: Entry['kind'], how: Entry['how'] = 'referrer'): Entry => ({ kind, how, signed: 'no' })

test('sameIdentity compares name, version and extraIdentity exactly', () => {
  expect(sameIdentity({ name: 'a', extraIdentity: { arch: 'amd64' } }, { name: 'a', extraIdentity: { arch: 'arm64' } })).toBe(false)
  expect(sameIdentity({ name: 'a' }, { name: 'a', extraIdentity: { arch: 'amd64' } })).toBe(false)
  expect(sameIdentity({ name: 'a', version: '1' }, { name: 'a' })).toBe(false)
  expect(sameIdentity({ name: 'a', extraIdentity: { x: '1', y: '2' } }, { name: 'a', extraIdentity: { y: '2', x: '1' } })).toBe(true)
  expect(sameIdentity(undefined, undefined)).toBe(true)
  expect(sameIdentity(undefined, { name: 'a' })).toBe(false)
})

test('labels', () => {
  expect(howLabel({ ...e('sbom', 'embedded'), resource: { name: 'sbom' } })).toBe('Embedded resource sbom')
  expect(howLabel({ ...e('sbom', 'buildx'), platform: 'linux/amd64' })).toBe('Buildx attestation (linux/amd64)')
  expect(howLabel(e('sbom', 'buildx'))).toBe('Buildx attestation')
  expect(howLabel(e('sbom', 'cosignTag'))).toBe('Cosign attestation tag')
  expect(howLabel(e('vex', 'labelLink'))).toBe('Label link')
  expect(signedLabel('signed')).toBe('Signed (not verified)')
  expect(signedLabel('component')).toBe('By component signature')
  expect(signedLabel('no')).toBe('No')
})

test('levelAnchor', () => {
  expect(levelAnchor()).toBe('attestations-component')
  expect(levelAnchor({ name: 'img' })).toBe('attestations-img')
  expect(levelAnchor({ name: 'img', version: '1.0+b', extraIdentity: { os: 'linux', arch: 'amd64' } })).toBe('attestations-img-1.0_b-arch=amd64-os=linux')
})

test('attestationName', () => {
  expect(attestationName({ ...e('slsa'), type: 'https://slsa.dev/provenance/v1' })).toBe('SLSA provenance')
  expect(attestationName({ ...e('slsa'), type: 'https://slsa.dev/verification_summary/v1' })).toBe('SLSA verification summary')
  expect(attestationName(e('cve'))).toBe('CVE scan')
})

test('detailsText', () => {
  expect(detailsText({ ...e('slsa'), buildLevel: 3, builder: 'https://github.com/actions/runner/github-hosted' })).toBe('Build L3 · https://github.com/actions/runner/github-hosted')
  expect(detailsText({ ...e('slsa'), format: 'in-toto' })).toBe('in-toto')
  expect(detailsText({ ...e('sbom'), format: 'CycloneDX', summary: { format: 'CycloneDX 1.6', packages: 214 } })).toBe('CycloneDX 1.6 · 214 packages')
  expect(detailsText({ ...e('vex'), summary: { format: 'OpenVEX 0.2', statements: 9, affected: 1 } })).toBe('OpenVEX 0.2 · 9 statements · 1 affected')
  expect(detailsText({ ...e('cve'), summary: { format: 'SARIF 2.1.0', tool: 'Trivy', findings: 37, importantFindings: 4, importantBasis: 'security-severity' } })).toBe('Trivy · SARIF 2.1.0 · 37 findings · 4 high or critical')
  expect(detailsText({ ...e('cve'), summary: { format: 'SARIF 2.1.0', findings: 2, importantFindings: 1, importantBasis: 'level' } })).toBe('SARIF 2.1.0 · 2 findings · 1 with level error')
  expect(detailsText({ ...e('sbom'), summary: { format: 'CycloneDX 1.6', packages: 14, statements: 56, affected: 5 } })).toBe('CycloneDX 1.6 · 14 packages · 56 statements · 5 affected')
  expect(detailsText({ ...e('fips'), format: 'x' })).toBe('x')
  expect(detailsText(e('fips'))).toBe('')
})

test('entryViewer', () => {
  expect(entryViewer({ ...e('cve'), digest: 'sha256:a', summary: { format: 'SARIF 2.1.0' } })).toBe('document')
  expect(entryViewer({ ...e('vex'), format: 'OpenVEX' })).toBeUndefined()
  expect(entryViewer({ ...e('vex'), format: 'OpenVEX', digest: 'sha256:a' })).toBe('document')
  // A VEX label linking a CycloneDX document: the summary says it was parsed.
  expect(entryViewer({ ...e('vex', 'labelLink'), digest: 'sha256:a', summary: { format: 'CycloneDX 1.6', statements: 3 } })).toBe('document')
  expect(entryViewer({ ...e('vex', 'labelLink'), digest: 'sha256:a', format: 'CSAF' })).toBeUndefined()
  expect(entryViewer({ ...e('sbom', 'embedded'), format: 'SPDX', digest: 'sha256:a' })).toBeUndefined()
  expect(entryViewer({ ...e('sbom'), summary: { format: 'CycloneDX 1.6' }, digest: 'sha256:a' })).toBe('document')
  expect(entryViewer({ ...e('sbom'), format: 'syft', digest: 'sha256:a' })).toBeUndefined()
  expect(entryViewer({ ...e('slsa'), digest: 'sha256:a' })).toBe('raw')
  expect(entryViewer(e('slsa'))).toBeUndefined()
})

const set: AttestationSet = {
  name: 'c',
  version: '1',
  repository: 'oci::h/c',
  checkedAt: '2026-01-01T00:00:00Z',
  levels: [
    { status: 'ok', entries: [{ ...e('vex', 'labelLink'), label: 'l' }] },
    { status: 'ok', resource: { name: 'img' }, entries: [e('sbom'), { ...e('sbom', 'buildx'), platform: 'linux/amd64' }, e('sbom', 'embedded')] },
  ],
}

test('levelFor and entriesOf', () => {
  expect(levelFor(set)?.entries).toHaveLength(1)
  expect(levelFor(set, { name: 'img' })?.resource?.name).toBe('img')
  expect(levelFor(set, { name: 'nope' })).toBeUndefined()
  expect(levelFor(undefined)).toBeUndefined()
  const l = levelFor(set, { name: 'img' })
  expect(entriesOf(l)).toHaveLength(1)
  expect(entriesOf(l, 'linux/amd64').map((x) => x.how)).toEqual(['buildx'])
  expect(entriesOf(undefined)).toEqual([])
})

test('labelEntry', () => {
  expect(labelEntry(set, undefined, 'l')?.how).toBe('labelLink')
  expect(labelEntry(set, undefined, 'other')).toBeUndefined()
  expect(labelEntry(set, { name: 'img' }, 'l')).toBeUndefined()
})
