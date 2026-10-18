import { screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { renderApp } from '../test/render'
import { server } from '../test/server'
import { componentJson } from '../test/fixtures'

const name = 'github.com/acme/app'
const ref = `oci::ghcr.io/acme/ocm//${name}:2.0.0`
const base = { name, version: '2.0.0', repository: 'oci::ghcr.io/acme/ocm', title: 'App', signed: false, responsibles: [], docs: [], contents: { resources: [], references: 0 }, resources: [], signatures: [] }
const res = (n: string, type: string) => ({ name: n, version: '2.0.0', type, relation: 'local' })
const sig = { name: 'default', digest: { hashAlgorithm: 'SHA-256', normalisationAlgorithm: 'jsonNormalisation/v4alpha1', value: 'x' }, signature: { algorithm: 'RSASSA-PSS', value: 'v', mediaType: 'm' } }

function serve(ov: object) {
  server.use(http.get('/v1/components/*', ({ request }) =>
    HttpResponse.json(new URL(request.url).pathname.includes('/overview/') ? ov : componentJson(name))))
}

test('unsigned without resources: access, inspect, transfer only', async () => {
  serve(base)
  renderApp(`/components/${name}?view=usage`)
  expect(await screen.findByRole('heading', { name: 'Configure access' })).toBeInTheDocument()
  expect(screen.getByText(/hostname: ghcr\.io/)).toBeInTheDocument()
  expect(screen.getByText(`ocm get cv ${ref}`)).toBeInTheDocument()
  expect(screen.getByText(`ocm get cv ${ref} -o widetree --recursive`)).toBeInTheDocument()
  expect(screen.getByText(`ocm transfer cv ${ref} <target-repository> --copy-resources --recursive`)).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'Verify' })).not.toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'Download a resource' })).not.toBeInTheDocument()
  expect(screen.getAllByRole('link', { name: 'Docs' })[0]).toHaveAttribute('href', 'https://ocm.software/docs/concepts/credential-system/')
})

test('signed: verify command and per-signature config', async () => {
  serve({ ...base, signed: true, signatures: [sig] })
  renderApp(`/components/${name}?view=usage`)
  expect(await screen.findByRole('heading', { name: 'Verify' })).toBeInTheDocument()
  expect(screen.getByText(`ocm verify cv ${ref}`)).toBeInTheDocument()
  expect(screen.getByText(/signature: default/)).toBeInTheDocument()
})

test('one resource: no select', async () => {
  serve({ ...base, resources: [res('image', 'ociImage')] })
  renderApp(`/components/${name}?view=usage`)
  expect(await screen.findByText(`ocm download resource ${ref} --identity name=image --output image`)).toBeInTheDocument()
  expect(screen.queryByRole('combobox', { name: 'Resource' })).not.toBeInTheDocument()
})

test('several resources: select, helm charts first, switches the command', async () => {
  serve({ ...base, resources: [res('image', 'ociImage'), res('chart', 'helmChart')] })
  const { user } = renderApp(`/components/${name}?view=usage`)
  const select = await screen.findByRole('combobox', { name: 'Resource' })
  const options = within(select).getAllByRole('option').map((o) => o.textContent)
  expect(options).toEqual(['chart · helmChart · 2.0.0', 'image · ociImage · 2.0.0'])
  expect(screen.getByText(`ocm download resource ${ref} --identity name=chart --output chart`)).toBeInTheDocument()
  await user.selectOptions(select, 'image · ociImage · 2.0.0')
  expect(screen.getByText(`ocm download resource ${ref} --identity name=image --output image`)).toBeInTheDocument()
})

test('Usage tab sits right after Docs, which is always shown', async () => {
  serve(base)
  renderApp(`/components/${name}`)
  const tabs = await screen.findAllByRole('tab')
  expect(tabs.map((t) => t.textContent)).toEqual(['Overview', 'Docs', 'Usage', 'Versions', 'Replication', 'Contents', 'Findings', 'Descriptor'])
})

test('air-gapped transfer: download into a local CTF archive, then upload from it', async () => {
  serve(base)
  renderApp(`/components/${name}?view=usage`)
  const section = (await screen.findByRole('heading', { name: 'Transfer: Air-Gap (CTF)' })).closest('section')!
  expect(within(section).getByText(`ocm transfer cv ${ref} ctf::./app-2.0.0.ctf --copy-resources --recursive`)).toBeInTheDocument()
  expect(within(section).getByText(`ocm transfer cv ctf::./app-2.0.0.ctf//${name}:2.0.0 <target-repository> --copy-resources --recursive`)).toBeInTheDocument()
  expect(within(section).getAllByRole('button', { name: 'Copy command' })).toHaveLength(2)
})

test('transfer headings name both ways', async () => {
  serve(base)
  renderApp(`/components/${name}?view=usage`)
  expect(await screen.findByRole('heading', { name: 'Transfer: Registry to Registry' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Transfer: Air-Gap (CTF)' })).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'Transfer' })).not.toBeInTheDocument()
})
