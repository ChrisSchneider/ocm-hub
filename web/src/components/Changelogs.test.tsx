import { screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { renderApp } from '../test/render'
import { server } from '../test/server'
import { serveVersions } from '../test/versions'

function serveChangelogs(o: { noChangelog?: boolean; size?: number; contentType?: string } = {}) {
  serveVersions()
  const d = (name: string, version: string, resources: object[]) => ({ meta: { schemaVersion: 'v2' }, component: { name, version, provider: 'acme', resources } })
  const cl = { name: 'changelog', version: '1', type: 'changelog' }
  server.use(
    http.get('/v1/components/:name/descriptors/:version', ({ request }) => {
      if (new URL(request.url).searchParams.get('compact') === 'true') return HttpResponse.json([])
      return HttpResponse.json([
        { location: 'oci::ghcr.io/acme', descriptor: d('acme.org/x', '1.1.0', o.noChangelog ? [] : [cl]) },
        ...(o.noChangelog ? [] : [{ location: 'oci::mirror.example/acme', descriptor: d('other.org/lib', '2.0.0', [cl]) }]),
      ])
    }),
    http.get('/v1/components/:name/resources/:version/:resource', ({ params }) => {
      if (params.version === '2.0.0') return HttpResponse.json({ errors: [{ code: 'BOOM', message: 'failed' }] }, { status: 500 })
      const headers: Record<string, string> = { 'Content-Type': o.contentType ?? 'text/markdown' }
      if (o.size) headers['Content-Length'] = String(o.size)
      return new HttpResponse('# 1.1.0\n\n- added <b>x</b>', { headers })
    }),
  )
}

test('expanding a version shows its changelog rendered as markdown and sub-component blocks', async () => {
  serveChangelogs()
  const { user } = renderApp('/components/acme.org/x?view=versions')
  const row = (await screen.findAllByRole('listitem'))[0]
  await user.click(within(row).getByRole('button', { name: 'Show changelog' }))
  expect(await within(row).findByRole('heading', { name: '1.1.0' })).toBeInTheDocument()
  expect(within(row).queryByText('x', { selector: 'b' })).not.toBeInTheDocument() // raw HTML not rendered
  const sub = within(row).getByRole('button', { name: /other\.org\/lib 2\.0\.0/ })
  expect(within(row).getByText('other registry')).toBeInTheDocument()
  await user.click(sub)
  expect(await within(row).findByRole('alert')).toBeInTheDocument() // its fetch failed; root block still there
  expect(within(row).getByRole('heading', { name: '1.1.0' })).toBeInTheDocument()
})

test('a version without changelogs says so', async () => {
  serveChangelogs({ noChangelog: true })
  const { user } = renderApp('/components/acme.org/x?view=versions')
  const row = (await screen.findAllByRole('listitem'))[0]
  await user.click(within(row).getByRole('button', { name: 'Show changelog' }))
  expect(await within(row).findByText('No changelog in this version or its references.')).toBeInTheDocument()
})

test('an oversized changelog is offered as a download', async () => {
  serveChangelogs({ size: 2 << 20 })
  const { user } = renderApp('/components/acme.org/x?view=versions')
  const row = (await screen.findAllByRole('listitem'))[0]
  await user.click(within(row).getByRole('button', { name: 'Show changelog' }))
  expect(await within(row).findByRole('link', { name: /Download changelog/ })).toHaveAttribute('href', expect.stringContaining('/resources/1.1.0/changelog'))
})

test('a plain-text changelog is shown preformatted', async () => {
  serveChangelogs({ contentType: 'text/plain' })
  const { user } = renderApp('/components/acme.org/x?view=versions')
  const row = (await screen.findAllByRole('listitem'))[0]
  await user.click(within(row).getByRole('button', { name: 'Show changelog' }))
  expect((await within(row).findByText(/# 1\.1\.0/)).tagName).toBe('PRE')
})
