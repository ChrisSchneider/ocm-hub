import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { renderApp } from '../test/render'
import { howTo, located, parseApiPath } from '../test/fixtures'
import { server } from '../test/server'

const overview = {
  name: 'ocm.software/how-to/product', title: 'How-to Product', provider: 'ocm.software', versionCount: 3,
  sources: [],
  locations: [{ repository: 'oci::ghcr.io/acme/ocm', latest: '2.0.0', versionCount: 2 }, { repository: 'oci::registry.example/mirror', versionCount: 1 }],
  categories: ['examples'], description: 'Bundle of backend, frontend and cache.', latest: '2.0.0', defaultVersion: '2.0.0', signed: false, referencedBy: [], referencedByCount: 0, links: { versions: '' },
}

const minimalOverview = {
  name: 'ocm.software/how-to/product', version: '2.0.0', repository: 'oci::ghcr.io/acme/ocm', title: 'How-to Product', description: 'Bundle of backend, frontend and cache.', signed: false, responsibles: [], docs: [],
  contents: { resources: [], references: 0 }, architectures: [], imagePlatformsRead: true, resources: [], references: [], signatures: [],
}

function useOverview(o: object = overview) {
  const paths: string[] = []
  server.use(
    http.get('/v1/components/*', ({ request }) => {
      const p = parseApiPath(request.url)
      if (new URL(request.url).pathname.includes('/overview/')) return HttpResponse.json(minimalOverview)
      if (p.kind === 'versions') return HttpResponse.json({ name: 'ocm.software/how-to/product', versions: [] })
      if (p.kind === 'component') {
        paths.push(new URL(request.url).pathname)
        return HttpResponse.json(o)
      }
      const d = { meta: { schemaVersion: 'v2' }, component: { name: 'ocm.software/how-to/product', version: p.version, provider: 'ocm.software' } }
      return HttpResponse.json(new URL(request.url).searchParams.get('recursive') === '-1' ? [{ location: 'oci::ghcr.io/acme/ocm', descriptor: d }] : d)
    }),
  )
  return paths
}

const versionsHandler = (fn: (u: URL) => Record<string, unknown>) =>
  http.get('/v1/components/*', ({ request }) => {
    const u = new URL(request.url)
    if (parseApiPath(request.url).kind !== 'versions') return undefined
    return HttpResponse.json(fn(u))
  })

const entry = (version: string, publishedAt = '2026-01-01T00:00:00Z', stable = true) => ({ version, publishedAt, stable, locations: ['oci::ghcr.io/acme/ocm'] })

test('top bar shows the title as h1 and no version button; the old reference line is gone', async () => {
  const paths = useOverview()
  renderApp('/components/ocm.software/how-to/product?view=contents')
  const banner = await screen.findByRole('banner')
  expect(await within(banner).findByRole('heading', { level: 1, name: 'How-to Product' })).toBeInTheDocument()
  expect(within(banner).queryByRole('button')).not.toBeInTheDocument()
  expect(within(banner).getByRole('link', { name: 'OCM Hub' })).toBeInTheDocument()
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/product 2.0.0' })
  await screen.findByRole('tree')
  expect(screen.queryByText('Bundle of backend, frontend and cache.')).not.toBeInTheDocument() // the description lives in the Overview
  expect(screen.queryByText('examples')).not.toBeInTheDocument() // categories are shown on catalog cards only
  expect(screen.queryByText((_, el) => el?.tagName === 'SPAN' && /\/\/\s*ocm\.software\/how-to\/product$/.test(el.textContent ?? ''))).not.toBeInTheDocument()
  expect(within(panel).queryByRole('button', { name: /ocm get cv/ })).not.toBeInTheDocument()
  expect(paths[0]).toBe('/v1/components/ocm.software%2Fhow-to%2Fproduct') // requests the encoded name
  expect(screen.queryByText('coming soon')).not.toBeInTheDocument()
  expect(screen.queryByPlaceholderText(/sub-components/)).not.toBeInTheDocument()
})

test('the catalog top bar has neither title nor version button', async () => {
  server.use(http.get('/v1/components', () => HttpResponse.json({ components: [], facets: { connectors: [], providers: [], categories: [] } })))
  renderApp('/')
  const banner = await screen.findByRole('banner')
  expect(within(banner).queryByRole('heading')).not.toBeInTheDocument()
  expect(within(banner).queryByRole('button')).not.toBeInTheDocument()
  expect(within(banner).queryByLabelText('Search in this component')).not.toBeInTheDocument()
})

test('the panel subtitle shows provider, registry and created time', async () => {
  useOverview()
  server.use(http.get('/v1/components/*', ({ request }) => {
    const p = parseApiPath(request.url)
    if (p.kind !== 'descriptor') return undefined
    return HttpResponse.json({ meta: { schemaVersion: 'v2' }, component: { name: 'ocm.software/how-to/product', version: p.version, provider: 'ocm.software', creationTime: '2020-01-02T03:04:05Z' } })
  }))
  renderApp('/components/ocm.software/how-to/product?view=contents')
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/product 2.0.0' })
  expect(await within(panel).findByText(/^by ocm\.software$/)).toBeInTheDocument()
  expect(within(panel).getByText('ghcr.io/acme/ocm')).toBeInTheDocument()
  expect(within(panel).getByText(/^Created .*ago$/)).toHaveAttribute('title', '2020-01-02T03:04:05Z')
})

test('the panel subtitle omits Created without a creation time', async () => {
  useOverview()
  renderApp('/components/ocm.software/how-to/product?view=contents')
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/product 2.0.0' })
  await within(panel).findByText(/^by ocm\.software$/)
  expect(screen.queryByText(/^Created/)).not.toBeInTheDocument()
})

test('typing in the component search sets ?q= with replace and mirrors it', async () => {
  useOverview()
  const { user, router } = renderApp('/components/ocm.software/how-to/product?view=contents')
  await user.type(await screen.findByLabelText('Search in this component'), 'cache')
  expect(router.state.matches.at(-1)?.search).toMatchObject({ q: 'cache' })
  await user.clear(screen.getByLabelText('Search in this component'))
  expect(router.state.matches.at(-1)?.search).not.toHaveProperty('q', 'cache')
})

test('the search field is filled from ?q=', async () => {
  useOverview()
  renderApp('/components/ocm.software/how-to/product?view=contents&q=abc')
  expect(await screen.findByLabelText('Search in this component')).toHaveValue('abc')
})

test('the ocm get cv command is in Usage, not in the Overview', async () => {
  useOverview()
  renderApp('/components/ocm.software/how-to/product?view=usage')
  expect(await screen.findByText('ocm get cv oci::ghcr.io/acme/ocm//ocm.software/how-to/product:2.0.0')).toBeInTheDocument()
})

test('badges a component from another registry', async () => {
  server.use(
    http.get('/v1/components/*', ({ request }) => {
      const u = new URL(request.url)
      if (u.pathname.includes('/overview/')) return HttpResponse.json({ code: 'VERSION_UNKNOWN', message: 'no overview in this test' }, { status: 404 })
      if (parseApiPath(request.url).kind === 'descriptor' && u.searchParams.get('recursive') !== '-1') return HttpResponse.json(howTo[0])
      if (u.searchParams.get('recursive') === '-1')
        return HttpResponse.json([
          { location: 'oci::ghcr.io/open-component-model', descriptor: howTo[0] },
          ...howTo.slice(1).map((d) => ({ location: 'oci::registry.example/other', descriptor: d })),
        ])
      return HttpResponse.json({ name: 'ocm.software/how-to/product', title: 'Product', latest: '2.0.0', defaultVersion: '2.0.0', versionCount: 1, locations: [{ repository: 'oci::ghcr.io/open-component-model', versionCount: 1 }], sources: [], links: { versions: '' } })
    }),
  )
  renderApp('/components/ocm.software/how-to/product?view=contents')
  expect((await screen.findAllByText(/from registry\.example\/other/)).length).toBeGreaterThan(0)
})

test('unknown component shows not found with a link back', async () => {
  server.use(http.get('/v1/components/*', () => HttpResponse.json({ errors: [{ code: 'COMPONENT_UNKNOWN', message: 'component unknown: acme.org/x', detail: {} }] }, { status: 404 })))
  renderApp('/components/acme.org/x')
  expect(await screen.findByText(/component not found/i)).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /back to the catalog/i })).toBeInTheDocument()
})

test('the overview reads ?repository= and Usage shows the command for it', async () => {
  const calls: string[] = []
  server.use(http.get('/v1/components/*', ({ request }) => {
    const u = new URL(request.url)
    calls.push(u.pathname + u.search)
    if (u.pathname.includes('/overview/')) return HttpResponse.json({ ...minimalOverview, version: '1.9.0', repository: 'oci::registry.example/mirror' })
    if (u.pathname.endsWith('/versions')) return HttpResponse.json({ name: 'ocm.software/how-to/product', versions: [] })
    return HttpResponse.json(overview)
  }))
  renderApp('/components/ocm.software/how-to/product?version=1.9.0&repository=oci%3A%3Aregistry.example%2Fmirror&view=usage')
  expect(await screen.findByText('ocm get cv oci::registry.example/mirror//ocm.software/how-to/product:1.9.0')).toBeInTheDocument()
  expect(calls.some((c) => c.includes('/overview/1.9.0') && c.includes('repository=oci%3A%3Aregistry.example%2Fmirror'))).toBe(true)
})

test('tabs switch views via ?view=', async () => {
  useOverview()
  const { user, router } = renderApp('/components/ocm.software/how-to/product')
  await user.click(await screen.findByRole('tab', { name: 'Contents' }))
  await waitFor(() => expect(router.state.matches.at(-1)?.search).toEqual({ view: 'contents' }))
  expect(await screen.findByRole('tree')).toBeInTheDocument()
  await user.click(screen.getByRole('tab', { name: 'Overview' }))
  await waitFor(() => expect(router.state.matches.at(-1)?.search).toEqual({}))
})

test('the overview is the default view; the in-component search is always there and switches to Contents', async () => {
  useOverview()
  const { user, router } = renderApp('/components/ocm.software/how-to/product')
  expect(await screen.findByText('Bundle of backend, frontend and cache.')).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: 'Overview', selected: true })).toBeInTheDocument()
  expect(screen.queryByRole('tree')).not.toBeInTheDocument()
  await user.type(screen.getByLabelText('Search in this component'), 'cache')
  await waitFor(() => expect(router.state.matches.at(-1)?.search).toMatchObject({ view: 'contents', q: 'cache' }))
})

test('the top bar shows the default version and its location without highlight', async () => {
  useOverview()
  renderApp('/components/ocm.software/how-to/product')
  const tag = await screen.findByRole('status', { name: 'Selected version' })
  await waitFor(() => expect(tag).toHaveTextContent('ghcr.io/acme/ocm'))
  expect(tag).toHaveTextContent('2.0.0')
  expect(tag).toHaveAttribute('data-default', 'true')
  expect(within(tag).queryByRole('button', { name: 'Switch to default' })).not.toBeInTheDocument()
})


// withResource: the root descriptor has a resource, so the Contents segments show.
function withResource() {
  useOverview()
  server.use(http.get('/v1/components/*', ({ request }) => {
    const p = parseApiPath(request.url)
    if (p.kind !== 'descriptor' || new URL(request.url).searchParams.get('recursive') === '-1') return undefined
    return HttpResponse.json({ meta: { schemaVersion: 'v2' }, component: { name: 'ocm.software/how-to/product', version: p.version, provider: 'ocm.software', resources: [{ name: 'chart', version: '1', type: 'helmChart' }] } })
  }))
}

test('?view=attestations opens Contents with the Attestations segment selected', async () => {
  withResource()
  renderApp('/components/ocm.software/how-to/product?view=attestations')
  expect(await screen.findByRole('radio', { name: 'Attestations', checked: true })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: 'Contents', selected: true })).toBeInTheDocument()
  expect(screen.queryByRole('tab', { name: 'Attestations' })).not.toBeInTheDocument()
})

test('?view=labels selects Labels', async () => {
  withResource()
  renderApp('/components/ocm.software/how-to/product?view=labels')
  expect(await screen.findByRole('radio', { name: 'Labels', checked: true })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: 'Contents', selected: true })).toBeInTheDocument()
})

test('?view=compliance opens Overview and there is no Compliance tab', async () => {
  useOverview()
  renderApp('/components/ocm.software/how-to/product?view=compliance')
  expect(await screen.findByText('Bundle of backend, frontend and cache.')).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: 'Overview', selected: true })).toBeInTheDocument()
  expect(screen.queryByRole('tab', { name: 'Compliance' })).not.toBeInTheDocument()
})
