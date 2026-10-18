import { screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { renderApp } from '../test/render'
import { server } from '../test/server'
import { componentJson } from '../test/fixtures'

const name = 'github.com/acme/app'
const base = { name, version: '2.0.0', repository: 'oci::ghcr.io/acme/ocm', title: 'App', signed: false, responsibles: [], docs: [], contents: { resources: [], references: 0 }, resources: [], signatures: [] }
const doc = (n: string, extraIdentity?: Record<string, string>) => ({ name: n, version: '2.0.0', type: 'documentation', relation: 'local', access: {}, extraIdentity })

function serve(ov: object, files: Record<string, { body: string; status?: number }> = {}) {
  server.use(http.get('/v1/components/*', ({ request }) => {
    const u = new URL(request.url)
    if (u.pathname.includes('/overview/')) return HttpResponse.json(ov)
    const m = u.pathname.match(/\/resources\/[^/]+\/([^/]+)$/)
    if (m) {
      const f = files[decodeURIComponent(m[1])]
      return new HttpResponse(f?.body ?? '', { status: f?.status ?? 200, headers: { 'Content-Type': 'text/markdown' } })
    }
    return HttpResponse.json(componentJson(name))
  }))
}

test('renders the documentation resource and drops unsafe/relative links', async () => {
  serve({ ...base, resources: [doc('guide')] }, { guide: { body: '# Guide\n\n[ok](https://example.com) [rel](./other.md) [js](javascript:alert(1))' } })
  renderApp(`/components/${name}?view=docs`)
  expect(await screen.findByRole('heading', { name: 'Guide' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'ok' })).toHaveAttribute('href', 'https://example.com')
  expect(screen.getByText('rel').closest('a')).not.toHaveAttribute('href')
  expect(screen.getByText('js').closest('a')).not.toHaveAttribute('href')
})

test('picker with two documentation resources; names collide → extra identity shown', async () => {
  serve({ ...base, resources: [doc('guide', { lang: 'en' }), doc('guide', { lang: 'de' })] }, { guide: { body: '# Hello' } })
  const { user } = renderApp(`/components/${name}?view=docs`)
  expect(await screen.findByRole('button', { name: 'guide (en)' })).toHaveAttribute('aria-pressed', 'true')
  await user.click(screen.getByRole('button', { name: 'guide (de)' }))
  expect(screen.getByRole('button', { name: 'guide (de)' })).toHaveAttribute('aria-pressed', 'true')
})

test('docs links grouped by kind; no resource still shows links', async () => {
  serve({ ...base, docs: [{ title: 'Home', url: 'https://acme.dev', kind: 'website' }, { title: 'API', url: 'https://acme.dev/api', kind: 'api' }, { title: 'Blog', url: 'https://blog.acme.dev' }] })
  renderApp(`/components/${name}?view=docs`)
  expect(await screen.findByRole('heading', { name: 'Website' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'API reference' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Links' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Blog' })).toHaveAttribute('target', '_blank')
})

test('failing documentation resource keeps the links', async () => {
  serve({ ...base, resources: [doc('guide')], docs: [{ title: 'Home', url: 'https://acme.dev', kind: 'website' }] }, { guide: { body: '', status: 502 } })
  renderApp(`/components/${name}?view=docs`)
  expect(await screen.findByRole('alert')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Home' })).toBeInTheDocument()
})

test('no docs: the tab is still shown', async () => {
  serve(base)
  renderApp(`/components/${name}`)
  expect(await screen.findByRole('tab', { name: 'Docs' })).toBeInTheDocument()
})

test('no docs: direct link shows empty state', async () => {
  serve(base)
  renderApp(`/components/${name}?view=docs`)
  expect(await screen.findByText('This component version has no documentation.')).toBeInTheDocument()
})

test('with docs: tab order', async () => {
  serve({ ...base, resources: [doc('guide')] }, { guide: { body: 'x' } })
  renderApp(`/components/${name}`)
  await waitFor(async () => expect((await screen.findAllByRole('tab')).map((t) => t.textContent)).toEqual(['Overview', 'Docs', 'Usage', 'Versions', 'Replication', 'Contents', 'Descriptor']))
})
