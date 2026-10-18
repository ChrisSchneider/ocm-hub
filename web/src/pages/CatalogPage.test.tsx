import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { renderApp } from '../test/render'
import { server } from '../test/server'

const list = {
  components: [
    { name: 'ocm.software/ocmcli', title: 'OCM CLI', categories: ['cli'], provider: 'ocm.software', latest: '0.30.0', defaultVersion: '0.30.0', signed: false, latestPublishedAt: '2026-10-01T00:00:00Z', description: 'The OCM command line', versionCount: 12, locations: ['oci::ghcr.io/open-component-model'] },
    { name: 'ocm.software/how-to/product', title: 'How-to Product', categories: ['examples'], provider: 'ocm.software', latest: '2.0.0', defaultVersion: '2.0.0', signed: false, versionCount: 3, locations: ['oci::ghcr.io/open-component-model', 'oci::registry.example/mirror'] },
  ],
  facets: {
    connectors: [{ key: 'oci::ghcr.io/open-component-model', title: 'ghcr.io/open-component-model', type: 'oci/v1', count: 2, groups: [] }, { key: 'oci::registry.example/mirror', title: 'registry.example/mirror', type: 'oci/v1', count: 1, groups: [] }],
    providers: [{ value: 'ocm.software', count: 2 }, { value: 'acme', count: 1 }],
    categories: [{ value: 'cli', count: 1 }, { value: 'examples', count: 1 }],
    architectures: [{ value: 'linux/amd64', count: 2 }, { value: 'linux/arm64', count: 1 }],
    replication: [{ value: 'single', count: 1 }, { value: 'replicated', count: 1 }],
  },
  total: 2,
}

let lastQuery: URLSearchParams
beforeEach(() =>
  server.use(
    http.get('/v1/components', ({ request }) => {
      lastQuery = new URL(request.url).searchParams
      return HttpResponse.json(list)
    }),
  ),
)

test('shows cards with latest version and locations', async () => {
  renderApp('/')
  const card = await screen.findByRole('link', { name: /How-to Product/ })
  expect(within(card).getByText(/2\.0\.0/)).toBeInTheDocument()
  expect(within(card).getByText(/2 locations/)).toBeInTheDocument()
  expect(within(screen.getByRole('link', { name: /OCM CLI/ })).queryByText(/location/)).not.toBeInTheDocument()
})

test('sends search and facet filters to the server', async () => {
  const { user } = renderApp('/')
  await screen.findByRole('link', { name: /OCM CLI/ })
  await user.type(screen.getByRole('searchbox', { name: /search/i }), 'command')
  await vi.waitFor(() => expect(lastQuery.get('q')).toBe('command'))
  await user.click(screen.getByRole('button', { name: 'Filter' }))
  await user.click(screen.getByRole('checkbox', { name: /registry\.example\/mirror/ }))
  await vi.waitFor(() => expect(lastQuery.getAll('connector')).toEqual(['oci::registry.example/mirror']))
})

test('architecture facet filters the list', async () => {
  const { user } = renderApp('/')
  await screen.findByRole('link', { name: /OCM CLI/ })
  await user.click(screen.getByRole('button', { name: 'Filter' }))
  await user.click(screen.getByRole('checkbox', { name: /linux\/arm64/ }))
  await vi.waitFor(() => expect(lastQuery.getAll('architecture')).toEqual(['linux/arm64']))
})

test('provider is single-select', async () => {
  const { user } = renderApp('/')
  await screen.findByRole('link', { name: /OCM CLI/ })
  await user.click(screen.getByRole('button', { name: 'Filter' }))
  await user.click(screen.getByRole('checkbox', { name: /ocm\.software \(2\)/ }))
  await vi.waitFor(() => expect(lastQuery.get('provider')).toBe('ocm.software'))
  await user.click(screen.getByRole('checkbox', { name: /acme/ }))
  await vi.waitFor(() => expect(lastQuery.get('provider')).toBe('acme'))
  expect(screen.getByRole('checkbox', { name: /ocm\.software \(2\)/ })).not.toBeChecked()
  expect(screen.getByRole('checkbox', { name: /acme/ })).toBeChecked()
  await user.click(screen.getByRole('checkbox', { name: /acme/ }))
  await vi.waitFor(() => expect(lastQuery.get('provider')).toBeNull())
})

test('the filter bar opens the filters, shows active ones as chips and the total', async () => {
  const { user } = renderApp('/')
  await screen.findByRole('link', { name: /OCM CLI/ })
  expect(screen.getByText('2 components')).toBeInTheDocument()
  expect(screen.queryByRole('dialog', { name: 'Filters' })).toBeNull()
  await user.click(screen.getByRole('button', { name: 'Filter' }))
  const panel = screen.getByRole('dialog', { name: 'Filters' })
  await user.click(within(panel).getByRole('checkbox', { name: /Replicated \(2\+ locations\)/ }))
  await vi.waitFor(() => expect(lastQuery.get('replication')).toBe('replicated'))
  await user.click(within(panel).getByRole('checkbox', { name: /Single location/ }))
  await vi.waitFor(() => expect(lastQuery.get('replication')).toBe('single')) // single-select
  await user.click(within(panel).getByRole('checkbox', { name: /registry\.example\/mirror/ }))
  await user.keyboard('{Escape}')
  expect(screen.queryByRole('dialog', { name: 'Filters' })).toBeNull()
  await user.click(screen.getByRole('button', { name: 'Remove filter registry.example/mirror' }))
  await vi.waitFor(() => expect(lastQuery.getAll('connector')).toEqual([]))
  await user.click(screen.getByRole('button', { name: 'Remove filter Single location' }))
  await vi.waitFor(() => expect(lastQuery.get('replication')).toBeNull())
})

test('loads the next page', async () => {
  server.use(
    http.get('/v1/components', ({ request }) => {
      const cursor = new URL(request.url).searchParams.get('cursor')
      return HttpResponse.json(cursor ? { components: [list.components[1]], facets: list.facets } : { components: [list.components[0]], next: 'c1', facets: list.facets })
    }),
  )
  const { user } = renderApp('/')
  await screen.findByRole('link', { name: /OCM CLI/ })
  await user.click(screen.getByRole('button', { name: /load more/i }))
  expect(await screen.findByRole('link', { name: /How-to Product/ })).toBeInTheDocument()
})

test('explains a missing index', async () => {
  server.use(http.get('/v1/components', () => HttpResponse.json({ errors: [{ code: 'INDEX_UNAVAILABLE', message: 'run cmd/sync', detail: {} }] }, { status: 503 })))
  renderApp('/')
  expect(await screen.findByText(/run cmd\/sync/)).toBeInTheDocument()
})

test('opens a component by name', async () => {
  const { user, router } = renderApp('/')
  await screen.findByRole('link', { name: /OCM CLI/ })
  await user.type(screen.getByRole('searchbox', { name: /search/i }), 'acme.org/not-in-catalog{Enter}')
  expect(router.state.location.pathname).toBe('/components/acme.org/not-in-catalog')
})

test('a card links to the component page', async () => {
  const { user, router } = renderApp('/')
  await user.click(await screen.findByRole('link', { name: /How-to Product/ }))
  expect(router.state.location.pathname).toBe('/components/ocm.software/how-to/product')
})

test('an empty index shows the sync hint and keeps open-by-name', async () => {
  server.use(http.get('/v1/components', () => HttpResponse.json({ components: [], facets: { connectors: [], providers: [], categories: [], architectures: [], replication: [] }, total: 0 })))
  renderApp('/?all=1')
  expect(await screen.findByText(/no components indexed yet/i)).toBeInTheDocument()
  expect(screen.getByRole('searchbox', { name: /search/i })).toBeInTheDocument()
  expect(screen.getByRole('checkbox', { name: 'Include sub-components' })).toBeChecked()
})

test('an empty top-level page says no match and keeps the toggle', async () => {
  server.use(http.get('/v1/components', () => HttpResponse.json({ components: [], facets: { connectors: [], providers: [], categories: [], architectures: [], replication: [] }, total: 0 })))
  renderApp('/')
  expect(await screen.findByText('No component matches.')).toBeInTheDocument()
  expect(screen.getByRole('checkbox', { name: 'Include sub-components' })).toBeInTheDocument()
  expect(screen.queryByText(/no components indexed yet/i)).not.toBeInTheDocument()
})

test('the OCM Hub link clears both ?q= and the input', async () => {
  const { user, router } = renderApp('/')
  await screen.findByRole('link', { name: /OCM CLI/ })
  const box = screen.getByRole('searchbox', { name: /search/i })
  await user.type(box, 'command')
  expect(router.state.location.search).toMatchObject({ q: 'command' })
  await user.click(screen.getByRole('link', { name: 'OCM Hub' }))
  expect(router.state.location.search).not.toHaveProperty('q', 'command')
  expect(screen.getByRole('searchbox', { name: /search/i })).toHaveValue('')
})

test('on a component page the top bar has no catalog search, so typing never leaves the component', async () => {
  server.use(http.get('/v1/components/*', () => HttpResponse.json({ name: 'acme.org/x', title: 'X', latest: '1.0.0', defaultVersion: '1.0.0', versionCount: 1, locations: [{ repository: 'oci::r', versionCount: 1 }], sources: [], links: { versions: '' } })))
  const { user, router } = renderApp('/components/acme.org/x?view=contents')
  const box = await screen.findByLabelText('Search in this component')
  expect(screen.queryByLabelText(/open a component by name/)).not.toBeInTheDocument()
  await user.type(box, 'abc{Enter}')
  expect(router.state.location.pathname).toBe('/components/acme.org/x')
})

test('without a query only top-level components are requested; the toggle shows all', async () => {
  const seen: string[] = []
  server.use(http.get('/v1/components', ({ request }) => {
    seen.push(new URL(request.url).searchParams.get('topLevel') ?? '')
    return HttpResponse.json({ components: [{ name: 'acme.org/a', title: 'acme.org/a', versionCount: 1, locations: ['oci::x'], signed: false }], facets: { connectors: [], providers: [], categories: [], architectures: [], replication: [] }, total: 0 })
  }))
  const { user, router } = renderApp('/')
  await screen.findByRole('link', { name: /acme\.org/ }) // the icon placeholder letter also reads "A"
  expect(seen.at(-1)).toBe('true')
  await user.click(screen.getByRole('checkbox', { name: 'Include sub-components' }))
  await waitFor(() => expect(seen.at(-1)).toBe(''))
  expect(router.state.matches.at(-1)?.search).toEqual({ q: undefined, all: true }) // location.search holds the raw string
})

test('the catalog also lists components that only have pre-releases', async () => {
  let pre = ''
  server.use(http.get('/v1/components', ({ request }) => {
    pre = new URL(request.url).searchParams.get('includePrerelease') ?? ''
    return HttpResponse.json({ components: [], facets: { connectors: [], providers: [], categories: [], architectures: [], replication: [] }, total: 0 })
  }))
  renderApp('/')
  await waitFor(() => expect(pre).toBe('true'))
})

test('a search query always searches all components', async () => {
  const seen: string[] = []
  server.use(http.get('/v1/components', ({ request }) => {
    seen.push(new URL(request.url).searchParams.get('topLevel') ?? '')
    return HttpResponse.json({ components: [], facets: { connectors: [], providers: [], categories: [], architectures: [], replication: [] }, total: 0 })
  }))
  renderApp('/?q=ecr')
  await waitFor(() => expect(seen.length).toBeGreaterThan(0))
  expect(seen.every((s) => s === '')).toBe(true)
})

test('the Sources facet filters by source key', async () => {
  const seen: URLSearchParams[] = []
  server.use(
    http.get('/v1/components', ({ request }) => {
      seen.push(new URL(request.url).searchParams)
      return HttpResponse.json({
        components: [],
        facets: {
          connectors: [
            { key: 'github:org:acme', title: 'Acme', type: 'github/v1', avatarUrl: 'https://avatars.example/acme', count: 3, groups: [] },
            { key: 'github:user:jane', title: 'jane', type: 'github/v1', count: 1, groups: [] },
          ],
          providers: [],
          categories: [],
          architectures: [],
          replication: [],
        },
        total: 0,
      })
    }),
  )
  const { user } = renderApp('/')
  await user.click(await screen.findByRole('button', { name: 'Filter' }))
  const facet = await screen.findByRole('group', { name: 'Sources' })
  expect(within(facet).getByText('(3)')).toBeInTheDocument()
  await user.click(within(facet).getByRole('checkbox', { name: /Acme/ }))
  await waitFor(() => expect(seen.at(-1)?.getAll('connector')).toEqual(['github:org:acme']))
  expect(screen.queryByRole('group', { name: 'Locations' })).toBeNull()
})

test('the sort select defaults to relevance (server default) and sends the chosen order', async () => {
  const { user } = renderApp('/')
  await screen.findByRole('link', { name: /OCM CLI/ })
  const select = screen.getByRole('combobox', { name: 'Sort' })
  expect(select).toHaveValue('relevance')
  expect(lastQuery.get('sort')).toBeNull()
  expect([...select.querySelectorAll('option')].map((o) => o.textContent)).toEqual(['Relevance', 'Recently updated', 'Name'])
  await user.selectOptions(select, 'name')
  await vi.waitFor(() => expect(lastQuery.get('sort')).toBe('name'))
  await user.selectOptions(select, 'updated')
  await vi.waitFor(() => expect(lastQuery.get('sort')).toBe('updated'))
})
