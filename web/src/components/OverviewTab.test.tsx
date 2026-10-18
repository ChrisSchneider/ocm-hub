import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { renderApp } from '../test/render'
import { attestationsNotCollected } from '../test/fixtures'
import { server } from '../test/server'

const component = {
  name: 'github.com/acme/app', title: 'App', provider: 'acme', latest: '1.1.0', defaultVersion: '1.1.0', versionCount: 2, signed: true,
  sources: [{ key: 'github:org:acme', title: 'Acme', type: 'github/v1', locations: ['oci::ghcr.io/acme', 'oci::mirror.example/acme'], groups: [] }],
  locations: [{ repository: 'oci::ghcr.io/acme', latest: '1.1.0', versionCount: 2 }, { repository: 'oci::mirror.example/acme', latest: '1.0.0', versionCount: 1 }],
  referencedBy: [{ name: 'github.com/acme/platform', title: 'Platform', latest: '3.0.0' }], referencedByCount: 1, links: { versions: '' },
}
const sig = { name: 'acme-signer', digest: { hashAlgorithm: 'SHA-256', normalisationAlgorithm: 'jsonNormalisation/v4alpha1', value: 'x' }, signature: { algorithm: 'RSASSA-PSS', value: 'v', mediaType: 'application/x-pem-file' } }
const full = {
  name: 'github.com/acme/app', version: '1.1.0', repository: 'oci::ghcr.io/acme', title: 'App', description: 'The app.', provider: 'acme', categories: ['tools'],
  signed: true, kubernetes: { constraint: '>=1.28', versions: ['1.28', '1.29', '1.31'], distributions: ['gardener', 'eks'] },
  storage: [{ accessMode: 'RWX', volumeMode: 'Filesystem', purpose: 'shared uploads' }, { accessMode: 'RWO', volumeMode: 'Block' }],
  responsibles: [{ type: 'githubUser', username: 'jane' }, { type: 'githubTeam', teamname: 'acme/core' }, { type: 'codeowners' }],
  riskProfile: { networkExposure: 'public' },
  docs: [{ title: 'Guide', url: 'https://example.com/guide', kind: 'tutorial' }, { title: 'Docs', url: 'https://example.com/docs', kind: 'docs' }],
  contents: { resources: [{ type: 'changelog', count: 1 }, { type: 'ociImage', count: 2 }], references: 1 },
  references: [{ name: 'db', version: '2.0.0', componentName: 'github.com/acme/db' }],
  architectures: [{ platform: 'linux/amd64', from: ['image', 'ocm'] }, { platform: 'linux/arm64', from: ['image'] }], imagePlatformsRead: false,
  sources: [{ name: 'src', version: '1.1.0', type: 'git', access: { type: 'gitHub', repoUrl: 'https://github.com/acme/app', commit: '0123456789abcdef' } }],
  resources: [
    { name: 'app', version: '1.1.0', type: 'ociImage', relation: 'external', extraIdentity: { arch: 'amd64' } },
    { name: 'app', version: '1.1.0', type: 'ociImage', relation: 'external', extraIdentity: { arch: 'arm64' } },
    { name: 'notes', version: '1.1.0', type: 'changelog', relation: 'local' },
  ],
  signatures: [sig],
}
const set = {
  name: 'github.com/acme/app', version: '1.1.0', repository: 'oci::ghcr.io/acme', checkedAt: '2026-01-01T00:00:00Z',
  levels: [{ status: 'ok', entries: [{ kind: 'slsa', how: 'referrer', signed: 'no', format: 'SLSA v1', buildLevel: 2 }] }],
}

// held: the locations the versions list reports for the overview's version.
const versionsOf = (version: string, held = ['oci::ghcr.io/acme']) => () =>
  HttpResponse.json({ name: 'github.com/acme/app', versions: [{ version, publishedAt: '2026-01-01T00:00:00Z', stable: true, locations: held }] })

function serve(ov: object, comp: object = component, attestations: () => Response = () => HttpResponse.json(set), versions: (u: URL) => Response = versionsOf((ov as { version: string }).version)) {
  const calls: string[] = []
  server.use(http.get('/v1/components/*', ({ request }) => {
    const u = new URL(request.url)
    calls.push(u.pathname + u.search)
    if (u.pathname.includes('/overview/')) return HttpResponse.json(ov)
    if (u.pathname.includes('/attestations/')) return attestations()
    if (u.pathname.endsWith('/versions')) return versions(u)
    return HttpResponse.json(comp)
  }))
  return calls
}

const rowOf = (label: string) => within(screen.getByRole('list', { name: 'Supply-chain evidence' })).getByRole('listitem', { name: label })
const pod = (name: string) => screen.getByRole('region', { name })
const facts = () => screen.getByRole('complementary', { name: 'Facts' })
const fact = (term: string) => within(facts()).getByText(term, { selector: 'dt' }).nextElementSibling as HTMLElement

test('header, pods, evidence and sidebar', async () => {
  serve(full)
  renderApp('/components/github.com/acme/app')
  expect(await screen.findByText('The app.')).toBeInTheDocument()
  expect(screen.queryByText(/ocm get cv/)).not.toBeInTheDocument() // no quick start
  const about = screen.getByRole('region', { name: 'About' })
  expect(within(about).getByRole('heading', { name: 'App' })).toBeInTheDocument()
  expect(within(about).getByText('acme')).toBeInTheDocument()
  expect(screen.queryByText(/Why it matters/)).not.toBeInTheDocument()
  expect(screen.queryByText(/public/)).not.toBeInTheDocument()

  // Contents: one entry per type, distinct names, a link to all of them.
  const contents = pod('Resources')
  expect(within(within(contents).getByRole('list', { name: 'OCI images' })).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['app'])
  expect(within(contents).getByRole('list', { name: 'Changelog' })).toHaveTextContent('notes')
  expect(within(contents).getByRole('list', { name: 'Reference' })).toHaveTextContent('db')
  expect(within(contents).getByRole('link', { name: 'All 4 →' })).toHaveAttribute('href', expect.stringContaining('view=contents'))

  const compat = pod('Compatibility')
  expect(within(compat).getByRole('list', { name: 'Kubernetes' })).toHaveTextContent('1.28–1.291.31')
  expect(within(compat).getByRole('list', { name: 'Distributions' })).toHaveTextContent('gardenereks')
  expect(within(compat).getByText('RWX filesystem')).toHaveAttribute('title', 'shared uploads')
  expect(within(compat).getByText('RWO block')).toBeInTheDocument()
  expect(within(compat).getByText('linux/arm64')).toHaveAttribute('title', 'from image')
  expect(within(compat).getByText('linux/amd64')).toHaveAttribute('title', 'from OCM and image')
  expect(within(compat).getByText(/image platforms not read/)).toHaveClass('text-slate-400')

  const signature = rowOf('OCM signature')
  expect(within(signature).getByText('acme-signer · RSASSA-PSS · certificate')).toBeInTheDocument()
  expect(within(signature).getByTitle('signed (not verified)')).toHaveClass('text-slate-400')
  expect(within(signature).getByRole('tooltip')).toHaveTextContent(/hasn't been tampered with/)
  const slsa = rowOf('Provenance')
  expect(await within(slsa).findByText(/Build Level 2/)).toBeInTheDocument()
  expect(within(slsa).getByRole('link', { name: 'github.com/acme/app @ 0123456' })).toHaveAttribute('href', 'https://github.com/acme/app/tree/0123456789abcdef')
  expect(within(rowOf('VEX')).getByText('Not attached')).toHaveClass('text-slate-400')
  expect(screen.getByText(/the hub verifies none of it/)).toBeInTheDocument()

  expect(within(facts()).getByRole('link', { name: 'Usage' })).toHaveAttribute('href', expect.stringContaining('view=usage'))
  expect(within(facts()).getByRole('link', { name: 'Docs' })).toHaveAttribute('href', expect.stringContaining('view=docs'))
  expect(within(facts()).getByRole('link', { name: 'Docs' })).not.toHaveClass('text-slate-400')
  expect(screen.getByRole('tab', { name: 'Docs' })).not.toHaveClass('text-slate-400')
  expect(fact('Version')).toHaveTextContent('1.1.0')
  expect(within(fact('Categories')).getByText('tools')).toBeInTheDocument()
  expect(within(fact('Responsibles')).getByRole('link', { name: '@jane' })).toHaveAttribute('href', 'https://github.com/jane')
  expect(within(fact('Responsibles')).getByRole('link', { name: 'acme/core' })).toHaveAttribute('href', 'https://github.com/orgs/acme/teams/core')
  expect(within(fact('Responsibles')).getByText('CODEOWNERS of the source repository')).toBeInTheDocument()
  expect(within(fact('Used by')).getByRole('link', { name: 'Platform' })).toBeInTheDocument()
})

test('evidence rows link with View', async () => {
  serve(full)
  renderApp('/components/github.com/acme/app')
  await screen.findByText('The app.')
  expect(within(rowOf('VEX')).queryByRole('link', { name: /^Show/ })).not.toBeInTheDocument()
  expect(within(rowOf('OCM signature')).getByRole('link', { name: 'Show OCM signature' })).toHaveAttribute('href', expect.stringContaining('view=descriptor'))
  expect(await within(rowOf('Provenance')).findByRole('link', { name: 'Show Provenance' })).toHaveAttribute('href', expect.stringMatching(/view=attestations.*#attestations-component$/))
  // Replication is a tab; the sidebar does not repeat it.
  expect(within(facts()).queryByRole('link', { name: 'Replication' })).not.toBeInTheDocument()
})

test('no green classes', async () => {
  serve(full)
  renderApp('/components/github.com/acme/app')
  await screen.findByText('The app.')
  await within(rowOf('Provenance')).findByText(/Build Level 2/)
  expect(document.body.innerHTML).not.toMatch(/(green|emerald)-\d/)
})

test('an empty version shows the gaps in grey', async () => {
  serve(
    { name: 'github.com/acme/app', version: '1.1.0', repository: 'oci::ghcr.io/acme', title: 'github.com/acme/app', signed: false, responsibles: [], docs: [], contents: { resources: [], references: 0 }, architectures: [], imagePlatformsRead: true, resources: [], references: [], sources: [], signatures: [] },
    { ...component, signed: false, referencedBy: [], referencedByCount: 0, locations: [component.locations[0]] },
    () => attestationsNotCollected([{ version: '1.0.0', repository: 'oci::ghcr.io/acme' }]),
  )
  renderApp('/components/github.com/acme/app')
  const about = await screen.findByRole('region', { name: 'About' })
  expect(within(about).getByText('Provider not declared')).toHaveClass('text-slate-400')
  expect(screen.queryByText('No description.')).not.toBeInTheDocument()
  expect(within(fact('Categories')).getByText('Not declared')).toHaveClass('text-slate-400')
  expect(within(fact('Responsibles')).getByText('Not declared')).toHaveClass('text-slate-400')
  expect(within(fact('Used by')).getByText('None')).toBeInTheDocument()
  expect(within(pod('Resources')).getByText('None')).toHaveClass('text-slate-400')
  expect(pod('Compatibility')).toHaveClass('border-dashed')
  expect(within(pod('Compatibility')).getByText('No Kubernetes versions, storage or other dependencies declared.')).toBeInTheDocument()
  expect(within(rowOf('OCM signature')).getByText('Not signed')).toBeInTheDocument()
  // No docs: Docs is grey but still opens.
  expect(within(facts()).getByRole('link', { name: 'Docs' })).toHaveClass('text-slate-400')
  expect(screen.getByRole('tab', { name: 'Docs' })).toHaveClass('text-slate-400')
  expect(within(fact('Locations')).getByText('ghcr.io/acme')).toHaveAttribute('title', 'Shown here')
  expect(within(fact('Locations')).queryByText(/location/)).not.toBeInTheDocument()
  expect(await within(rowOf('VEX')).findByText('Not checked')).toHaveClass('text-slate-400')
  expect(screen.getByText(/Attestations are not collected for this version/)).toBeInTheDocument()
})

test('a component with only pre-releases shows its newest pre-release', async () => {
  const calls = serve({ ...full, version: '2.0.0-rc.1' }, { ...component, latest: undefined, defaultVersion: '2.0.0-rc.1' })
  renderApp('/components/github.com/acme/app')
  expect(await screen.findByText('No stable version yet — showing the newest pre-release.')).toBeInTheDocument()
  expect(calls.some((c) => c.includes('/overview/2.0.0-rc.1'))).toBe(true)
})

const three = { ...component, locations: [...component.locations, { repository: 'oci::old.example/acme', latest: '1.1.0', versionCount: 1 }] }

test('an older version held in two of three locations', async () => {
  const calls = serve({ ...full, version: '1.0.0' }, three, () => HttpResponse.json(set), versionsOf('1.0.0', ['oci::ghcr.io/acme', 'oci::mirror.example/acme']))
  const { user, router } = renderApp('/components/github.com/acme/app?version=1.0.0')
  const chips = await screen.findByRole('list', { name: 'Locations' })
  await waitFor(() => expect(within(chips).getByText('old.example/acme')).toHaveAttribute('title', '1.0.0 is not at this location'))
  expect(within(chips).getByText('ghcr.io/acme')).toHaveAttribute('title', 'Shown here')
  expect(within(chips).getByRole('button', { name: 'mirror.example/acme' })).toBeEnabled()
  expect(calls.some((c) => c.endsWith('/versions?semverConstraint=%3D1.0.0&includePrerelease=true'))).toBe(true)
  await user.click(within(chips).getByRole('button', { name: 'mirror.example/acme' }))
  await waitFor(() => expect(router.state.location.search).toMatchObject({ version: '1.0.0', repository: 'oci::mirror.example/acme' }))
})

test('a failed version lookup leaves the other locations plain', async () => {
  const calls = serve(full, three, () => HttpResponse.json(set), () => HttpResponse.json({ errors: [{ code: 'BAD_REQUEST', message: 'bad constraint', detail: {} }] }, { status: 400 }))
  renderApp('/components/github.com/acme/app')
  await screen.findByText('The app.')
  await waitFor(() => expect(calls.some((c) => c.includes('/versions?'))).toBe(true))
  const chips = screen.getByRole('list', { name: 'Locations' })
  expect(within(chips).getByRole('button', { name: 'old.example/acme' })).toBeDisabled()
  expect(within(chips).getByRole('button', { name: 'mirror.example/acme' })).toBeDisabled()
})

test('links into Contents keep version and repository but drop the selection and query', async () => {
  serve(full)
  const { user, router } = renderApp('/components/github.com/acme/app?version=1.1.0&repository=oci%3A%3Aghcr.io%2Facme&select=x%3A1&q=abc')
  await screen.findByText('The app.')
  await user.click(within(pod('Resources')).getByRole('link', { name: 'All 4 →' }))
  await waitFor(() => expect(router.state.location.search).toEqual({ version: '1.1.0', repository: 'oci::ghcr.io/acme', view: 'contents' }))
})

function serveOverviewError() {
  server.use(http.get('/v1/components/*', ({ request }) => {
    const u = new URL(request.url)
    if (u.pathname.includes('/overview/')) return HttpResponse.json({ code: 'VERSION_UNKNOWN', message: 'nope' }, { status: 404 })
    if (u.pathname.includes('/attestations/')) return attestationsNotCollected()
    return HttpResponse.json(component)
  }))
}

test('overview error while not inspecting shows no Switch to default', async () => {
  serveOverviewError()
  renderApp('/components/github.com/acme/app')
  expect(await screen.findByRole('alert')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Switch to default' })).not.toBeInTheDocument()
})

test('overview error while inspecting offers Switch to default which clears inspect state', async () => {
  serveOverviewError()
  const { user, router } = renderApp('/components/github.com/acme/app?version=9.9.9&repository=oci%3A%3Amirror.example%2Facme')
  expect(await screen.findByRole('alert')).toBeInTheDocument()
  // the top bar's inspect label has one too; the error state's own is the last
  await user.click(screen.getAllByRole('button', { name: 'Switch to default' }).at(-1)!)
  await waitFor(() => expect(router.state.location.search).toEqual({}))
})
