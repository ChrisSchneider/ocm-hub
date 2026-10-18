import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { componentJson, howTo, located, overviewMissing, parseApiPath } from '../test/fixtures'
import { renderApp } from '../test/render'
import { server } from '../test/server'

const product = 'ocm.software/how-to/product'

// contentRows: the item and group rows of the contents table (not the header or continuation lines).
const contentRows = (panel: HTMLElement) => within(panel).getAllByRole('row', { name: /^(source|resource|group) / })
const findContentRows = async (panel: HTMLElement) => within(panel).findAllByRole('row', { name: /^(source|resource|group) / })

function mockApi() {
  const calls: Array<{ recursive: string | null; compact: string | null }> = []
  server.use(
    http.get('/v1/components/*', ({ request }) => {
      const p = parseApiPath(request.url)
      if (p.kind === 'overview') return overviewMissing()
      if (p.kind === 'component') return HttpResponse.json(componentJson(p.name))
      const url = new URL(request.url)
      calls.push({ recursive: url.searchParams.get('recursive'), compact: url.searchParams.get('compact') })
      if (request.headers.get('Accept')?.includes('yaml')) return new HttpResponse(`component:\n  name: ${p.name}\n`, { headers: { 'Content-Type': 'application/yaml' } })
      if (url.searchParams.get('recursive') === '-1') return HttpResponse.json(located(howTo))
      const d = howTo.find((x) => x.component.name === p.name && x.component.version === p.version)
      return d ? HttpResponse.json(d) : HttpResponse.json({ errors: [{ code: 'VERSION_UNKNOWN', message: 'nope', detail: {} }] }, { status: 404 })
    }),
  )
  return calls
}

test('one compact recursive call builds the tree; first level expanded', async () => {
  const calls = mockApi()
  renderApp(`/components/${product}?view=contents`)
  const tree = await screen.findByRole('tree', { name: 'Components' })
  const items = await within(tree).findAllByRole('treeitem')
  expect(items.map((i) => i.getAttribute('aria-label'))).toEqual([
    'ocm.software/how-to/product 2.0.0', 'ocm.software/how-to/backend 3.1.0', 'ocm.software/how-to/frontend 1.5.0', 'ocm.software/how-to/cache 2.0.0',
  ])
  expect(calls.filter((c) => c.recursive === '-1')).toEqual([{ recursive: '-1', compact: 'true' }])
})

test('the tree sidebar is titled "Component tree" with the number of components', async () => {
  mockApi()
  renderApp(`/components/${product}?view=contents`)
  const sidebar = await screen.findByRole('complementary', { name: /component tree/i })
  expect(await within(sidebar).findByRole('heading', { name: /component tree \(4\)/i })).toBeInTheDocument()
  expect(within(sidebar).getByRole('tree', { name: 'Components' })).toBeInTheDocument()
})

test('sub-trees expand without further requests; shared children appear under each parent', async () => {
  const calls = mockApi()
  const { user } = renderApp(`/components/${product}?view=contents`)
  const tree = await screen.findByRole('tree', { name: 'Components' })
  const before = calls.length
  await user.click(await within(tree).findByRole('button', { name: 'Expand ocm.software/how-to/backend' }))
  expect(within(tree).getAllByRole('treeitem', { name: 'ocm.software/how-to/cache 2.0.0' })).toHaveLength(2)
  expect(calls.length).toBe(before)
})

test('selecting a sub-component shows its contents, sources first', async () => {
  mockApi()
  const { user, router } = renderApp(`/components/${product}?view=contents`)
  const item = await screen.findByRole('treeitem', { name: 'ocm.software/how-to/backend 3.1.0' })
  await user.click(within(item).getByRole('button', { name: /^backend/ }))
  expect(router.state.location.search).toMatchObject({ select: 'ocm.software/how-to/backend:3.1.0' })
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  expect(within(panel).getByText(/ghcr\.io\/acme\/ocm/)).toBeInTheDocument()
  const rows = await findContentRows(panel)
  expect(rows.map((r) => r.getAttribute('aria-label'))).toEqual(['source src', 'group api-image', 'resource chart'])
  expect(within(rows[2]).getByRole('link', { name: 'Download chart' })).toHaveAttribute('href', '/v1/components/ocm.software%2Fhow-to%2Fbackend/resources/3.1.0/chart')
  expect(within(rows[0]).getByRole('link', { name: 'Open source src' })).toHaveAttribute('href', 'https://github.com/acme/backend/tree/a1b2c3')
})

test('type chips filter the contents', async () => {
  mockApi()
  const { user } = renderApp(`/components/${product}?select=ocm.software%2Fhow-to%2Fbackend%3A3.1.0&view=contents`)
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  await user.click(await within(panel).findByRole('button', { name: 'helmChart 1' }))
  expect(contentRows(panel).map((r) => r.getAttribute('aria-label'))).toEqual(['resource chart'])
})

test('source chip filters to sources; type chips count resources only', async () => {
  mockApi()
  const { user } = renderApp(`/components/${product}?select=ocm.software%2Fhow-to%2Fbackend%3A3.1.0&view=contents`)
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  expect(within(panel).queryByRole('button', { name: /^git / })).toBeNull()
  expect(await within(panel).findByRole('button', { name: 'ociImage 2' })).toBeInTheDocument()
  await user.click(within(panel).getByRole('button', { name: 'source 1' }))
  expect(contentRows(panel).map((r) => r.getAttribute('aria-label'))).toEqual(['source src'])
})

test('ociImage rows open a docker pull dialog instead of downloading', async () => {
  mockApi()
  const { user } = renderApp(`/components/${product}?select=ocm.software%2Fhow-to%2Fbackend%3A3.1.0&view=contents`)
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  await user.click(await within(panel).findByRole('button', { name: 'Expand api-image' }))
  const row = within(panel).getByRole('row', { name: 'resource arch=amd64, os=linux' })
  expect(within(row).queryByRole('link', { name: /Download/ })).toBeNull()
  const pull = within(row).getByRole('button', { name: 'docker pull api-image' })
  await user.click(pull)
  const dialog = await screen.findByRole('dialog', { name: 'Pull image' })
  expect(within(dialog).getByText('docker pull ghcr.io/acme/api:3.1.0')).toBeInTheDocument()
  await user.click(within(dialog).getByRole('button', { name: /copy/ }))
  expect(await navigator.clipboard.readText()).toBe('docker pull ghcr.io/acme/api:3.1.0')
  await user.keyboard('{Escape}')
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(pull).toHaveFocus()
})

test('ociImage without an image reference keeps the download link', async () => {
  mockApi()
  const noRef = { ...howTo[1], component: { ...howTo[1].component, resources: [{ name: 'blobimg', version: '3.1.0', type: 'ociImage', access: { type: 'localBlob' } }] } }
  server.use(http.get('/v1/components/*', ({ request }) => {
    if (parseApiPath(request.url).kind !== 'descriptor') return undefined
    return HttpResponse.json(new URL(request.url).searchParams.get('recursive') === '-1' ? located([howTo[0], noRef, howTo[2], howTo[3]]) : noRef)
  }))
  renderApp(`/components/${product}?select=ocm.software%2Fhow-to%2Fbackend%3A3.1.0&view=contents`)
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  expect(await within(panel).findByRole('link', { name: 'Download blobimg' })).toBeInTheDocument()
  expect(within(panel).queryByRole('button', { name: /docker pull/ })).toBeNull()
})

test('Descriptor is a page tab showing the YAML; Contents has no inner tabs', async () => {
  mockApi()
  const { user, router } = renderApp(`/components/${product}?view=contents`)
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/product 2.0.0' })
  expect(within(panel).queryByRole('tab')).not.toBeInTheDocument()
  await user.click(within(screen.getByRole('tablist', { name: 'Component views' })).getByRole('tab', { name: 'Descriptor' }))
  await waitFor(() => expect(router.state.matches.at(-1)?.search).toMatchObject({ view: 'descriptor' }))
  expect(await screen.findByText(/name: ocm.software\/how-to\/product/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Copy descriptor' })).toBeInTheDocument()
})

test('contents rows expand from the keyboard', async () => {
  mockApi()
  const { user } = renderApp(`/components/${product}?select=ocm.software%2Fhow-to%2Fbackend%3A3.1.0&view=contents`)
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  const btn = await within(panel).findByRole('button', { name: 'Details chart' })
  expect(btn).toHaveAttribute('aria-expanded', 'false')
  btn.focus()
  await user.keyboard('{Enter}')
  expect(btn).toHaveAttribute('aria-expanded', 'true')
  expect(within(panel).getByText(/type: helmChart/)).toBeInTheDocument()
})

test('contents filter resets when another component is selected', async () => {
  mockApi()
  const { user } = renderApp(`/components/${product}?select=ocm.software%2Fhow-to%2Fbackend%3A3.1.0&view=contents`)
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  await user.click(await within(panel).findByRole('button', { name: 'helmChart 1' }))
  expect(contentRows(panel)).toHaveLength(1)
  const tree = screen.getByRole('tree', { name: 'Components' })
  const root = within(tree).getByRole('treeitem', { name: 'ocm.software/how-to/product 2.0.0' })
  await user.click(within(root).getByRole('button', { name: /^product/ }))
  await screen.findByRole('region', { name: 'ocm.software/how-to/product 2.0.0' })
  const back = within(tree).getByRole('treeitem', { name: 'ocm.software/how-to/backend 3.1.0' })
  await user.click(within(back).getByRole('button', { name: /^backend/ }))
  const again = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  expect(await findContentRows(again)).toHaveLength(3)
  expect(within(again).getByRole('button', { name: /^all 4/ })).toHaveAttribute('aria-pressed', 'true')
})

test('a pinned repository is sent for the root component, not for selected sub-components', async () => {
  const seen: string[] = []
  server.use(
    http.get('/v1/components/*', ({ request }) => {
      const p = parseApiPath(request.url)
      const url = new URL(request.url)
      if (p.kind === 'overview') return overviewMissing()
      if (p.kind === 'component') return HttpResponse.json(componentJson(p.name))
      seen.push(`descriptor ${p.name} ${url.searchParams.get('recursive') ?? ''} ${url.searchParams.get('repository')}`)
      if (url.searchParams.get('recursive') === '-1') return HttpResponse.json(located(howTo))
      return HttpResponse.json(howTo.find((x) => x.component.name === p.name)!)
    }),
  )
  const repo = encodeURIComponent('oci::registry.example/mirror')
  const { user } = renderApp(`/components/${product}?repository=${repo}&view=contents`)
  await screen.findByRole('tree', { name: 'Components' })
  expect(seen).toContain(`descriptor ${product} -1 oci::registry.example/mirror`)
  expect(seen).toContain(`descriptor ${product}  oci::registry.example/mirror`)

  const item = await screen.findByRole('treeitem', { name: 'ocm.software/how-to/backend 3.1.0' })
  await user.click(within(item).getByRole('button', { name: /^backend/ }))
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  await findContentRows(panel)
  expect(seen).toContain('descriptor ocm.software/how-to/backend  null')
  expect(within(panel).getAllByRole('link', { name: 'Download chart' })[0].getAttribute('href')).not.toContain('repository')
})

test('selecting a reference from another registry reads it from there', async () => {
  const seen: string[] = []
  server.use(
    http.get('/v1/components/*', ({ request }) => {
      const p = parseApiPath(request.url)
      const url = new URL(request.url)
      if (p.kind === 'overview') return overviewMissing()
      if (p.kind === 'component') return HttpResponse.json(componentJson(p.name))
      if (url.searchParams.get('recursive') === '-1')
        return HttpResponse.json([{ location: 'oci::ghcr.io/a', descriptor: howTo[0] }, ...howTo.slice(1).map((descriptor) => ({ location: 'oci::registry.example/other', descriptor }))])
      seen.push(`${p.name} ${url.searchParams.get('repository')}`)
      return HttpResponse.json(howTo.find((x) => x.component.name === p.name)!)
    }),
  )
  const { user, router } = renderApp(`/components/${product}?view=contents`)
  const item = await screen.findByRole('treeitem', { name: 'ocm.software/how-to/backend 3.1.0' })
  await user.click(within(item).getByRole('button', { name: /^backend/ }))
  await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  expect(router.state.location.search).toMatchObject({ select: 'ocm.software/how-to/backend:3.1.0', selectRepository: 'oci::registry.example/other' })
  await vi.waitFor(() => expect(seen).toContain('ocm.software/how-to/backend oci::registry.example/other'))
})

test('downloads of the pinned root carry the repository', async () => {
  const root = { ...howTo[0], component: { ...howTo[0].component, resources: [{ name: 'doc', version: '2.0.0', type: 'blob' }] } }
  server.use(
    http.get('/v1/components/*', ({ request }) => {
      const p = parseApiPath(request.url)
      if (p.kind === 'overview') return overviewMissing()
      if (p.kind === 'component') return HttpResponse.json(componentJson(p.name))
      return HttpResponse.json(new URL(request.url).searchParams.get('recursive') === '-1' ? located([root]) : root)
    }),
  )
  renderApp(`/components/${product}?repository=oci%3A%3Aghcr.io%2Facme%2Focm&view=contents`)
  const link = await screen.findByRole('link', { name: 'Download doc' })
  expect(link).toHaveAttribute('href', `/v1/components/ocm.software%2Fhow-to%2Fproduct/resources/2.0.0/doc?repository=oci%3A%3Aghcr.io%2Facme%2Focm`)
})

test('an unresolvable reference shows the error and falls back to the root with its direct references', async () => {
  server.use(
    http.get('/v1/components/*', ({ request }) => {
      const p = parseApiPath(request.url)
      if (p.kind === 'overview') return overviewMissing()
      if (p.kind === 'component') return HttpResponse.json(componentJson(p.name))
      if (new URL(request.url).searchParams.get('recursive') === '-1') {
        return HttpResponse.json({ errors: [{ code: 'COMPONENT_UNKNOWN', message: 'reference frontend: component unknown: ocm.software/how-to/frontend', detail: {} }] }, { status: 404 })
      }
      return HttpResponse.json(howTo[0])
    }),
  )
  renderApp(`/components/${product}?view=contents`)
  const tree = await screen.findByRole('tree', { name: 'Components' })
  const items = await within(tree).findAllByRole('treeitem')
  expect(items.map((i) => i.getAttribute('aria-label'))).toEqual([
    'ocm.software/how-to/product 2.0.0', 'ocm.software/how-to/backend 3.1.0', 'ocm.software/how-to/frontend 1.5.0', 'ocm.software/how-to/cache 2.0.0',
  ])
  expect(screen.getByText(/reference frontend: component unknown/)).toBeInTheDocument()
})

test('search filters the tree to matches and their ancestors, and the contents list', async () => {
  mockApi()
  const { user, router } = renderApp(`/components/${product}?view=contents`)
  await screen.findByRole('tree', { name: 'Components' })
  await user.type(screen.getByLabelText('Search in this component'), 'cache')
  expect(router.state.location.search).toMatchObject({ q: 'cache' })
  const tree = screen.getByRole('tree', { name: 'Components' })
  await waitFor(() =>
    expect(within(tree).getAllByRole('treeitem').map((i) => i.getAttribute('aria-label'))).toEqual([
      'ocm.software/how-to/product 2.0.0', 'ocm.software/how-to/backend 3.1.0', 'ocm.software/how-to/cache 2.0.0', 'ocm.software/how-to/cache 2.0.0',
    ]))
  expect(within(tree).queryByRole('treeitem', { name: /frontend/ })).not.toBeInTheDocument()
})

test('search filters the contents by name, type, version and identity; chips apply on top', async () => {
  mockApi()
  const { user } = renderApp(`/components/${product}?select=ocm.software%2Fhow-to%2Fbackend%3A3.1.0&q=chart&view=contents`)
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  expect((await findContentRows(panel)).map((r) => r.getAttribute('aria-label'))).toEqual(['resource chart']) // name and type
  await user.clear(screen.getByLabelText('Search in this component'))
  await user.type(screen.getByLabelText('Search in this component'), 'arm64')
  await waitFor(() => expect(contentRows(panel).map((r) => r.getAttribute('aria-label'))).toEqual(['resource api-image'])) // identity
  await user.clear(screen.getByLabelText('Search in this component'))
  await user.type(screen.getByLabelText('Search in this component'), 'api')
  await waitFor(() => expect(contentRows(panel).map((r) => r.getAttribute('aria-label'))).toEqual(['group api-image'])) // both, grouped
  await user.click(within(panel).getByRole('button', { name: /^helmChart/ }))
  expect(await within(panel).findByText('No match in this component.')).toBeInTheDocument()
})

test('downloads carry the extraIdentity of the resource', async () => {
  mockApi()
  server.use(http.get('/v1/components/*', ({ request }) => {
    const p = parseApiPath(request.url)
    if (p.kind !== 'descriptor') return undefined
    if (request.headers.get('Accept')?.includes('yaml') || new URL(request.url).searchParams.get('recursive')) return HttpResponse.json(located(howTo))
    return HttpResponse.json({
      meta: { schemaVersion: 'v2' },
      component: { name: p.name, version: p.version, provider: 'x', resources: [{ name: 'cli', version: '1.0.0', type: 'executable', extraIdentity: { os: 'linux', arch: 'amd64' } }] },
    })
  }))
  renderApp(`/components/${product}?view=contents`)
  const link = await screen.findByRole('link', { name: 'Download cli' })
  expect(link).toHaveAttribute('href', '/v1/components/ocm.software%2Fhow-to%2Fproduct/resources/2.0.0/cli?extraIdentity.os=linux&extraIdentity.arch=amd64')
})

test('the tree follows the panel in document order (right of it)', async () => {
  mockApi()
  renderApp(`/components/${product}?view=contents`)
  const tree = await screen.findByRole('tree', { name: 'Components' })
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/product 2.0.0' })
  expect(panel.compareDocumentPosition(tree) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
})

test('a selected sub-component known to the hub can be opened as its own page; the root has no Open', async () => {
  mockApi()
  const { user, router } = renderApp(`/components/${product}?view=contents`)
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/product 2.0.0' })
  await within(panel).findByText(/^by ocm\.software$/)
  expect(within(panel).queryByRole('link', { name: /^Open/ })).not.toBeInTheDocument()
  expect(within(panel).queryByRole('button', { name: /ocm get cv/ })).not.toBeInTheDocument()
  const item = await screen.findByRole('treeitem', { name: 'ocm.software/how-to/backend 3.1.0' })
  await user.click(within(item).getByRole('button', { name: /^backend/ }))
  const sub = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  await user.click(await within(sub).findByRole('link', { name: 'Open ocm.software/how-to/backend 3.1.0' }))
  await waitFor(() => expect(router.state.location.pathname).toBe('/components/ocm.software/how-to/backend'))
  expect(router.state.matches.at(-1)?.search).toMatchObject({ version: '3.1.0' })
})

test('the contents view segment is kept in ?view=', async () => {
  mockApi()
  const { user, router } = renderApp(`/components/${product}?select=ocm.software%2Fhow-to%2Fbackend%3A3.1.0&view=contents`)
  const panel = await screen.findByRole('region', { name: 'ocm.software/how-to/backend 3.1.0' })
  const group = await within(panel).findByRole('radiogroup', { name: 'Contents view' })
  expect(within(group).getByRole('radio', { name: 'Contents' })).toHaveAttribute('aria-checked', 'true')
  await user.click(within(group).getByRole('radio', { name: 'Labels' }))
  await waitFor(() => expect(router.state.location.search).toMatchObject({ view: 'labels' }))
})
