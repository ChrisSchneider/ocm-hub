import { useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from '@tanstack/react-router'
import { delay, http, HttpResponse } from 'msw'
import type { OcmDescriptor } from '../api/ocm'
import type { AttestationSet } from '../lib/attestations'
import type { View } from '../lib/contentsRows'
import { VEX_LABEL } from '../lib/labels'
import { parseComponentSearch } from '../router'
import { server } from '../test/server'
import { ContentsList } from './ContentsList'

const name = 'github.com/acme/app'
const descriptor = (resources: object[], extra: object = {}): OcmDescriptor =>
  ({ meta: { schemaVersion: 'v2' }, component: { name, version: '1.0.0', provider: 'acme', resources, ...extra } }) as OcmDescriptor

const entry = (kind: string, how = 'referrer', extra = {}) => ({ kind, how, signed: 'no', ...extra })
const set = (levels: object[]) => ({ name, version: '1.0.0', repository: 'oci::ghcr.io/acme', checkedAt: '2026-10-01T00:00:00Z', levels }) as AttestationSet

const img = { name: 'web', type: 'ociImage', version: '1.0.0', access: { imageReference: 'ghcr.io/acme/web:1' } }
const webId = { name: 'web', version: '1.0.0' }
const cli = (os: string, architecture: string) => ({ name: 'cli', type: 'executable', version: '1.0.0', extraIdentity: { os, architecture }, access: { type: 'localBlob' } })
const bom = { name: 'bom', type: 'sbom', version: '1.0.0', access: { type: 'localBlob', mediaType: 'application/vnd.cyclonedx+json' } }

interface Opts { view?: View; onView?: (v: View) => void; repository?: string; url?: string; attestations?: AttestationSet | (() => Response | Promise<Response>) }

// show renders the list in a router (NotCollected links) with its view in local state; it
// answers the attestation set from opts and counts the set requests.
function show(d: OcmDescriptor, opts: Opts = {}) {
  const calls = { set: 0 }
  server.use(
    http.get('/v1/components/:name/attestations/:version', () => {
      calls.set++
      const a = opts.attestations
      return typeof a === 'function' ? a() : HttpResponse.json(a ?? set([]))
    }),
  )
  function Harness() {
    const [view, setView] = useState<View>(opts.view ?? 'contents')
    return <ContentsList descriptor={d} repository={opts.repository} view={view} onView={(v) => { opts.onView?.(v); setView(v) }} />
  }
  const root = createRootRoute()
  const index = createRoute({ getParentRoute: () => root, path: '/', component: Harness })
  const comp = createRoute({ getParentRoute: () => root, path: '/components/$', validateSearch: parseComponentSearch, component: () => <p>component page</p> })
  const router = createRouter({ routeTree: root.addChildren([index, comp]), history: createMemoryHistory({ initialEntries: [opts.url ?? '/'] }) })
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return { router, user, calls }
}

const cells = (row: HTMLElement) => within(row).getAllByRole('cell').map((c) => c.textContent)
// bodyRows: the rows of a view's table without the header row.
const bodyRows = async (label: string) => within(await screen.findByRole('table', { name: label })).getAllByRole('row').slice(1)
const segment = (label: string) => within(screen.getByRole('radiogroup', { name: 'Contents view' })).getByRole('radio', { name: label })

test('contents view columns: version before type, no chips', async () => {
  const { user } = show(descriptor([{ ...img, extraIdentity: { arch: 'amd64' } }]))
  const table = await screen.findByRole('table', { name: 'Contents' })
  expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Name', 'Version', 'Type', 'Identity', 'Actions'])
  const row = within(table).getByRole('row', { name: 'resource web' })
  expect(cells(row)).toEqual(['web▸', '1.0.0', 'ociImage', 'arch=amd64', ''])
  expect(row.className).toMatch(/grid-cols-/)
  expect(within(row).getByRole('button', { name: 'docker pull web' })).toBeInTheDocument()
  expect(screen.queryByText('component itself')).not.toBeInTheDocument()
  expect(screen.queryByRole('link', { name: /attached|linked/ })).not.toBeInTheDocument()
  await user.click(within(row).getByRole('button', { name: 'Details web' }))
  expect(screen.getByText(/imageReference: ghcr.io\/acme\/web:1/)).toBeInTheDocument()
})

test('segmented control calls onView', async () => {
  const onView = vi.fn()
  const { user } = show(descriptor([img]), { onView })
  await screen.findByRole('table', { name: 'Contents' })
  const group = screen.getByRole('radiogroup', { name: 'Contents view' })
  expect(within(group).getAllByRole('radio').map((r) => r.textContent)).toEqual(['Contents', 'Labels', 'Attestations'])
  expect(segment('Contents')).toHaveAttribute('aria-checked', 'true')
  await user.click(segment('Labels'))
  expect(onView).toHaveBeenCalledWith('labels')
  expect(await screen.findByRole('table', { name: 'Labels' })).toBeInTheDocument()
  expect(segment('Labels')).toHaveAttribute('aria-checked', 'true')
})

test('resource group expands and its children appear in all views', async () => {
  const { user } = show(descriptor([cli('linux', 'amd64'), cli('linux', 'arm64'), img]))
  const toggle = await screen.findByRole('button', { name: 'Expand cli' })
  expect(toggle).toHaveAttribute('aria-expanded', 'false')
  expect(toggle).toHaveTextContent('▸ 2')
  expect((await bodyRows('Contents')).map((r) => r.getAttribute('aria-label'))).toEqual(['group cli', 'resource web'])
  expect(within(screen.getByRole('row', { name: 'group cli' })).queryByRole('link')).not.toBeInTheDocument()
  await user.click(toggle)
  expect((await bodyRows('Contents')).map((r) => r.getAttribute('aria-label'))).toEqual(['group cli', 'resource linux/amd64', 'resource linux/arm64', 'resource web'])
  expect(within(screen.getByRole('row', { name: 'resource linux/arm64' })).getByRole('link', { name: 'Download cli' })).toHaveAttribute('href', `/v1/components/${encodeURIComponent(name)}/resources/1.0.0/cli?extraIdentity.os=linux&extraIdentity.architecture=arm64`)
  await user.click(segment('Labels'))
  expect(await screen.findByRole('row', { name: 'resource linux/amd64' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Expand cli' })).toHaveAttribute('aria-expanded', 'true')
  await user.click(segment('Attestations'))
  expect(await screen.findByRole('row', { name: 'resource linux/arm64' })).toBeInTheDocument()
})

test('labels view: component row, one line per label, name only on first line, — for none', async () => {
  const web = { ...img, labels: [{ name: 'a', value: 'x' }, { name: 'b', value: [1, 2] }] }
  const chart = { name: 'chart', type: 'helmChart', version: '1.0.0' }
  show(descriptor([web, chart], { labels: [{ name: 'team', value: 'core' }] }), { view: 'labels' })
  const table = await screen.findByRole('table', { name: 'Labels' })
  expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Name', 'Label', 'Value', 'Actions'])
  const rows = await bodyRows('Labels')
  expect(rows.map((r) => cells(r).slice(0, 3))).toEqual([
    ['component itself', 'team', 'core'],
    ['web', 'a', 'x'],
    ['', 'b', '1, 2'],
    ['chart', '—', ''],
  ])
  expect(within(rows[2]).getByRole('cell', { name: '1, 2' })).toHaveAttribute('title', '1, 2')
})

test('labels view: object value shows {…} and opens a JSON modal', async () => {
  const { user } = show(descriptor([{ ...img, labels: [{ name: 'cfg', value: { verify: { level: 3 } } }] }]), { view: 'labels' })
  const row = (await bodyRows('Labels'))[1]
  expect(cells(row).slice(0, 3)).toEqual(['web', 'cfg', '{…}'])
  await user.click(within(row).getByRole('button', { name: 'View cfg' }))
  const dialog = await screen.findByRole('dialog', { name: 'cfg' })
  expect(within(dialog).getByText(/"level": 3/)).toBeInTheDocument()
})

test('labels view: VEX label opens the attestation by digest', async () => {
  let doc = ''
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', ({ request }) => {
    doc = decodeURIComponent(request.url)
    return HttpResponse.json({ format: 'OpenVEX', vex: { statements: [] } })
  }))
  const vex = entry('vex', 'labelLink', { label: VEX_LABEL, url: 'https://example.com/vex.json', digest: 'sha256:abc', format: 'OpenVEX 0.2' })
  const { user, calls } = show(descriptor([img], { labels: [{ name: VEX_LABEL, value: 'https://example.com/vex.json' }] }), { view: 'labels', attestations: set([{ status: 'ok', entries: [vex] }]) })
  const row = (await bodyRows('Labels'))[0]
  expect(cells(row).slice(0, 3)).toEqual(['component itself', VEX_LABEL, 'https://example.com/vex.json'])
  expect(within(row).getByRole('button', { name: `Download ${VEX_LABEL}` })).toBeInTheDocument()
  expect(calls.set).toBe(0)
  await user.click(within(row).getByRole('button', { name: `View ${VEX_LABEL}` }))
  expect(await screen.findByRole('dialog', { name: 'VEX' })).toBeInTheDocument()
  await waitFor(() => expect(doc).toContain('/attestations/1.0.0/sha256:abc?as=document'))
  expect(calls.set).toBe(1)
  expect(within(row).getByRole('link', { name: `Download ${VEX_LABEL}` })).toHaveAttribute('href', `/v1/components/${encodeURIComponent(name)}/attestations/1.0.0/sha256%3Aabc`)
})

test('labels view: VEX label without digest links the original URL', async () => {
  const vex = entry('vex', 'labelLink', { label: VEX_LABEL, url: 'https://example.com/vex.json' })
  const { user } = show(descriptor([{ ...img, labels: [{ name: VEX_LABEL, value: 'https://example.com/vex.json' }] }]), { view: 'labels', attestations: set([{ resource: webId, status: 'ok', entries: [vex] }]) })
  const row = (await bodyRows('Labels'))[1]
  await user.click(within(row).getByRole('button', { name: `View ${VEX_LABEL}` }))
  const link = await within(row).findByRole('link', { name: 'Original document' })
  expect(link).toHaveAttribute('href', 'https://example.com/vex.json')
  expect(within(row).queryByRole('button')).not.toBeInTheDocument()
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

test('labels view: a VEX label URL that is not https is shown as text', async () => {
  const vex = entry('vex', 'labelLink', { label: VEX_LABEL, url: 'http://example.com/vex.json' })
  const { user } = show(descriptor([{ ...img, labels: [{ name: VEX_LABEL, value: 'http://example.com/vex.json' }] }]), { view: 'labels', attestations: set([{ resource: webId, status: 'ok', entries: [vex] }]) })
  const row = (await bodyRows('Labels'))[1]
  await user.click(within(row).getByRole('button', { name: `View ${VEX_LABEL}` }))
  expect(await within(row).findByText('http://example.com/vex.json', { selector: 'span' })).toBeInTheDocument()
  expect(within(row).queryByRole('link', { name: 'Original document' })).not.toBeInTheDocument()
})

test('attestations view requests the set only when opened', async () => {
  const { user, calls } = show(descriptor([img]), { attestations: async () => { await delay(30); return HttpResponse.json(set([])) } })
  await screen.findByRole('table', { name: 'Contents' })
  await user.click(segment('Labels'))
  await screen.findByRole('table', { name: 'Labels' })
  expect(calls.set).toBe(0)
  await user.click(segment('Attestations'))
  expect(await screen.findByText('Loading attestations…')).toBeInTheDocument()
  await waitFor(() => expect(screen.queryByText('Loading attestations…')).not.toBeInTheDocument())
  expect(calls.set).toBe(1)
})

test('attestations view: name, predicate type, details, signed icon, eye only with viewer', async () => {
  const slsa = entry('slsa', 'referrer', { type: 'https://slsa.dev/provenance/v1', buildLevel: 3, builder: 'gha', signed: 'signed', digest: 'sha256:1' })
  const sbom = entry('sbom', 'referrer', { type: 'https://cyclonedx.org/bom', format: 'CycloneDX 1.6', summary: { format: 'CycloneDX 1.6', packages: 5 }, digest: 'sha256:2' })
  show(descriptor([img]), { view: 'attestations', attestations: set([{ resource: webId, status: 'ok', entries: [slsa, sbom] }]) })
  const table = await screen.findByRole('table', { name: 'Attestations' })
  expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Name', 'Attestation', 'Predicate type', 'Details', 'Signed', 'View'])
  await screen.findByText('SLSA provenance')
  const rows = await bodyRows('Attestations')
  expect(rows.map(cells)).toEqual([
    ['component itself', '—', '', '', '', ''],
    ['web▸', 'SLSA provenance', 'https://slsa.dev/provenance/v1', 'Build L3 · gha', '', ''],
    ['', 'SBOM', 'https://cyclonedx.org/bom', 'CycloneDX 1.6 · 5 packages', '', ''],
  ])
  const predicate = within(rows[1]).getByRole('cell', { name: 'https://slsa.dev/provenance/v1' })
  expect(predicate.className).toMatch(/font-mono/)
  expect(predicate).toHaveAttribute('title', 'https://slsa.dev/provenance/v1')
  expect(within(rows[1]).getByTitle('signed (not verified)')).toHaveClass('text-slate-400')
  expect(within(rows[2]).queryByTitle('signed (not verified)')).not.toBeInTheDocument()
  expect(within(rows[0]).queryByRole('button', { name: /^View/ })).not.toBeInTheDocument()
  expect(within(rows[1]).getByRole('button', { name: 'View SLSA provenance' })).toBeInTheDocument()
  expect(within(rows[2]).getByRole('button', { name: 'View SBOM' })).toBeInTheDocument()
})

test('embedded SBOM is not listed in attestations', async () => {
  const embedded = entry('sbom', 'embedded', { format: 'CycloneDX 1.6', resource: { name: 'bom', version: '1.0.0' } })
  show(descriptor([bom]), { view: 'attestations', attestations: set([{ status: 'ok', entries: [embedded] }]) })
  await waitFor(() => expect(screen.queryByText('Loading attestations…')).not.toBeInTheDocument())
  const rows = await bodyRows('Attestations')
  expect(rows.map((r) => cells(r).slice(0, 2))).toEqual([['component itself', '—'], ['bom', '—']])
})

test('group parent shows per resource; failed level message under the table, not red', async () => {
  const levels = [{ resource: webId, status: 'failed', error: 'unauthorized', entries: [] }, { resource: { name: 'web2', version: '1.0.0' }, status: 'ok', entries: [entry('slsa', 'buildx', { platform: 'linux/amd64' })] }]
  show(descriptor([cli('linux', 'amd64'), cli('linux', 'arm64'), img, { ...img, name: 'web2' }]), { view: 'attestations', attestations: set(levels) })
  const message = await screen.findByText("Couldn't check Resource web 1.0.0: unauthorized")
  expect(message).toHaveClass('text-slate-600')
  expect(screen.getByRole('table', { name: 'Attestations' })).not.toContainElement(message)
  const rows = await bodyRows('Attestations')
  expect(rows.map((r) => cells(r).slice(0, 2))).toEqual([['component itself', '—'], ['cli▸ 2', 'per resource'], ['web▸', '—'], ['web2▸', 'on platforms']])
  expect(document.body.innerHTML).not.toMatch(/(red|green|emerald)-\d/)
})

test('not collected message under the table', async () => {
  const notCollected = () => HttpResponse.json({ errors: [{ code: 'NOT_COLLECTED', message: 'not collected', detail: { collected: [{ version: '0.9.0', repository: 'oci::ghcr.io/acme' }] } }] }, { status: 404 })
  show(descriptor([img]), { view: 'attestations', attestations: notCollected })
  const message = await screen.findByText(/Attestations are not collected for this version/)
  expect(screen.getByRole('table', { name: 'Attestations' })).not.toContainElement(message)
  expect(screen.getByRole('link', { name: '0.9.0 at ghcr.io/acme' })).toBeInTheDocument()
})

test('sbom resource eye opens the SBOM viewer', async () => {
  let asParam: string | null = ''
  server.use(http.get('/v1/components/:name/resources/:version/:resource', ({ request }) => {
    asParam = new URL(request.url).searchParams.get('as')
    return HttpResponse.json({ format: 'CycloneDX 1.6', sbom: { packages: [{ name: 'left-pad', version: '1.3.0', license: 'MIT', type: 'npm' }] } })
  }))
  const { user } = show(descriptor([bom]))
  const row = await screen.findByRole('row', { name: 'resource bom' })
  expect(within(row).getByRole('link', { name: 'Download bom' })).toBeInTheDocument()
  await user.click(within(row).getByRole('button', { name: 'View bom' }))
  const dialog = await screen.findByRole('dialog', { name: 'bom' })
  expect(await within(dialog).findByText('left-pad')).toBeInTheDocument()
  expect(asParam).toBe('document')
})

test('oscal resource has download but no eye', async () => {
  show(descriptor([{ name: 'cd', type: 'oscal', version: '1.0.0', access: { type: 'localBlob', mediaType: 'application/json' } }]))
  const row = await screen.findByRole('row', { name: 'resource cd' })
  expect(within(row).getByRole('link', { name: 'Download cd' })).toBeInTheDocument()
  expect(within(row).queryByRole('button', { name: 'View cd' })).not.toBeInTheDocument()
})

test('level anchor id on the first line', async () => {
  const scroll = vi.fn()
  const original = Element.prototype.scrollIntoView
  Element.prototype.scrollIntoView = scroll
  try {
    const entries = [entry('slsa', 'referrer', { type: 'https://slsa.dev/provenance/v1' }), entry('vex', 'referrer', { format: 'OpenVEX' })]
    show(descriptor([img]), { view: 'attestations', url: '/#attestations-web-1.0.0', attestations: set([{ resource: webId, status: 'ok', entries }]) })
    await screen.findByText('SLSA provenance')
    const anchored = document.getElementById('attestations-web-1.0.0')
    expect(anchored).toBe(screen.getByRole('row', { name: 'resource web' }))
    expect(document.getElementById('attestations-component')).toBe(screen.getByRole('row', { name: 'component itself' }))
    await waitFor(() => expect(scroll).toHaveBeenCalled())
    expect(scroll.mock.contexts.at(-1)).toBe(anchored)
  } finally {
    Element.prototype.scrollIntoView = original
  }
})

test('an SBOM with an artifact reference sits right under the resource it describes', async () => {
  const described = { ...bom, labels: [{ name: 'ocm.software/artifactReference', version: 'v1', value: { identitySelector: { name: 'web' } } }] }
  const chart = { name: 'chart', type: 'helmChart', version: '1.0.0', access: { type: 'localBlob' } }
  show(descriptor([described, chart, img]))
  const rows = await bodyRows('Contents')
  expect(rows.map((r) => r.getAttribute('aria-label'))).toEqual(['resource chart', 'resource web', 'resource bom'])
  expect(rows[2]).toHaveAttribute('data-depth', '1')
  expect(within(rows[2]).getByText('SBOM of web')).toBeInTheDocument()
})

test('SBOMs referencing other SBOMs (chains, cycles) are never hidden; only non-SBOM targets nest', async () => {
  const ref = (n: string) => [{ name: 'ocm.software/artifactReference', version: 'v1', value: { identitySelector: { name: n } } }]
  const a = { name: 'a', type: 'sbom', version: '1.0.0', access: { type: 'localBlob' }, labels: ref('b') }
  const b = { name: 'b', type: 'sbom', version: '1.0.0', access: { type: 'localBlob' }, labels: ref('a') }
  const c = { name: 'c', type: 'sbom', version: '1.0.0', access: { type: 'localBlob' }, labels: ref('b') }
  show(descriptor([img, a, b, c]))
  const rows = await bodyRows('Contents')
  expect(rows.map((r) => r.getAttribute('aria-label'))).toEqual(['resource web', 'resource a', 'resource b', 'resource c'])
  expect(rows.map((r) => r.getAttribute('data-depth'))).toEqual(['0', '0', '0', '0'])
})

const sha = 'sha256:' + 'ab'.repeat(32)
const sha2 = 'sha256:' + 'cd'.repeat(32)
const imageIndex = (platforms: { platform: string; digest: string }[]) => ({ repository: 'ghcr.io/acme/web', digest: 'sha256:idx', mediaType: 'application/vnd.oci.image.index.v1+json', platforms })
const serveImage = (body: () => Response | Promise<Response>) => server.use(http.get('/v1/components/:name/images/:version/:resource', body))

test('image row expands to platforms with digest, pull and eye', async () => {
  serveImage(async () => { await delay(30); return HttpResponse.json(imageIndex([{ platform: 'linux/amd64', digest: sha }, { platform: 'linux/arm64', digest: sha2 }])) })
  const { user } = show(descriptor([img]))
  const index = await screen.findByRole('row', { name: 'resource web' })
  expect(within(index).queryByRole('button', { name: /^View/ })).not.toBeInTheDocument()
  await user.click(within(index).getByRole('button', { name: 'Expand web' }))
  expect(await screen.findByText('Loading platforms…')).toBeInTheDocument()
  const amd = await screen.findByRole('row', { name: 'platform linux/amd64' })
  expect(amd).toHaveAttribute('data-depth', '1')
  expect(cells(amd)).toEqual(['└linux/amd64', 'sha256:' + 'ab'.repeat(6), '', '', ''])
  expect(within(amd).getByText('sha256:' + 'ab'.repeat(6))).toHaveAttribute('title', sha)
  expect(within(index).getByRole('button', { name: 'Expand web' })).toHaveTextContent('▾ 2')
  await user.click(within(amd).getByRole('button', { name: 'docker pull web linux/amd64' }))
  expect(await screen.findByText(`docker pull ghcr.io/acme/web@${sha}`)).toBeInTheDocument()
})

test('single-manifest image shows one platform row', async () => {
  serveImage(() => HttpResponse.json(imageIndex([{ platform: 'linux/amd64', digest: sha }])))
  const { user } = show(descriptor([img]))
  await user.click(await screen.findByRole('button', { name: 'Expand web' }))
  expect(await screen.findAllByRole('row', { name: /^platform / })).toHaveLength(1)
})

test('a single manifest without os/arch shows its row as — and titles it by the resource name', async () => {
  serveImage(() => HttpResponse.json(imageIndex([{ platform: '', digest: sha }])))
  server.use(http.get('/v1/components/:name/images/:version/:resource/:digest', () => HttpResponse.json({ digest: sha, platform: '', size: 10, layers: [] })))
  const { user } = show(descriptor([img]))
  await user.click(await screen.findByRole('button', { name: 'Expand web' }))
  const row = await screen.findByRole('row', { name: 'platform —' })
  expect(cells(row)).toEqual(['└—', 'sha256:' + 'ab'.repeat(6), '', '', ''])
  await user.click(within(row).getByRole('button', { name: 'docker pull web' }))
  expect(await screen.findByText(`docker pull ghcr.io/acme/web@${sha}`)).toBeInTheDocument()
  await user.click(within(row).getByRole('button', { name: 'View web' }))
  expect(await screen.findByRole('dialog', { name: 'web' })).toBeInTheDocument()
})

test('attestations view shows a platform without os/arch as —', async () => {
  serveImage(() => HttpResponse.json(imageIndex([{ platform: '', digest: sha }])))
  const entries = [entry('sbom', 'referrer', { type: 'application/spdx+json' })]
  const { user } = show(descriptor([img]), { view: 'attestations', attestations: set([{ resource: webId, status: 'ok', entries }]) })
  expect(await screen.findByText(/SBOM/)).toBeInTheDocument()
  await user.click(within(screen.getByRole('row', { name: 'resource web' })).getByRole('button', { name: 'Expand web' }))
  const row = await screen.findByRole('row', { name: 'platform —' })
  expect(cells(row).slice(0, 2)).toEqual(['└—', '—'])
  expect(row).not.toHaveAttribute('id')
  expect(within(row).queryByRole('button', { name: /^View/ })).not.toBeInTheDocument()
})

test('platform lookup error shows in the child row', async () => {
  serveImage(() => HttpResponse.json({ errors: [{ code: 'UPSTREAM', message: 'registry unavailable' }] }, { status: 502 }))
  const { user } = show(descriptor([img, bom]))
  await user.click(await screen.findByRole('button', { name: 'Expand web' }))
  const msg = await screen.findByText('registry unavailable')
  expect(msg.className).toMatch(/text-slate-600/)
  expect(screen.getByRole('row', { name: 'resource bom' })).toBeInTheDocument()
})

test('labels view hides the image expand button', async () => {
  show(descriptor([img]), { view: 'labels' })
  await screen.findByRole('row', { name: 'resource web' })
  expect(screen.queryByRole('button', { name: 'Expand web' })).not.toBeInTheDocument()
})

test('attestations view lists buildx entries per platform', async () => {
  const error = vi.spyOn(console, 'error').mockImplementation(() => {})
  serveImage(() => HttpResponse.json(imageIndex([{ platform: 'linux/amd64', digest: sha }, { platform: 'linux/arm64', digest: sha2 }])))
  const entries = [entry('slsa', 'referrer', { type: 'https://slsa.dev/provenance/v1', platform: 'linux/amd64' })]
  const { user } = show(descriptor([img]), { view: 'attestations', attestations: set([{ resource: webId, status: 'ok', entries }]) })
  const index = await screen.findByRole('row', { name: 'resource web' })
  expect(await within(index).findByText('on platforms')).toBeInTheDocument()
  await user.click(within(index).getByRole('button', { name: 'Expand web' }))
  const amd = await screen.findByRole('row', { name: 'platform linux/amd64' })
  expect(within(amd).getByText('SLSA provenance')).toBeInTheDocument()
  const arm = screen.getByRole('row', { name: 'platform linux/arm64' })
  expect(within(arm).getByText('—')).toBeInTheDocument()
  expect(error).not.toHaveBeenCalled()
  error.mockRestore()
})

test('platform eye opens image details', async () => {
  serveImage(() => HttpResponse.json(imageIndex([{ platform: 'linux/amd64', digest: sha }])))
  let path = ''
  server.use(http.get('/v1/components/:name/images/:version/:resource/:digest', ({ request }) => {
    path = new URL(request.url).pathname
    return HttpResponse.json({ digest: sha, platform: 'linux/amd64', size: 10, layers: [] })
  }))
  const { user } = show(descriptor([img]))
  await user.click(await screen.findByRole('button', { name: 'Expand web' }))
  await user.click(await screen.findByRole('button', { name: 'View web linux/amd64' }))
  expect(await screen.findByRole('dialog', { name: 'web linux/amd64' })).toBeInTheDocument()
  await waitFor(() => expect(path).toContain('/images/1.0.0/web/'))
})
