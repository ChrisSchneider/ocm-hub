import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import { server } from '../../test/server'
import { ViewerPanel, type ViewerScope, type ViewerTarget } from './ViewerPanel'

const scope: ViewerScope = { name: 'acme.org/app', version: '1.0.0', repository: 'oci::ghcr.io/acme' }

function show(target: ViewerTarget, onClose = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={queryClient}>
      <ViewerPanel scope={scope} target={target} onClose={onClose} />
    </QueryClientProvider>,
  )
  return { user, onClose }
}

// bodyRows: the table rows without the header row.
const bodyRows = (label: string) => within(screen.getByRole('table', { name: label })).getAllByRole('row').slice(1)

const documentError = (code: string, status: number, detail: object = {}) =>
  HttpResponse.json({ errors: [{ code, message: code.toLowerCase(), detail }] }, { status })

test('sbom viewer shows stats, filters packages and links the download', async () => {
  let asParam: string | null = ''
  server.use(http.get('/v1/components/:name/resources/:version/:resource', ({ request }) => {
    asParam = new URL(request.url).searchParams.get('as')
    return HttpResponse.json({
      format: 'CycloneDX 1.6',
      sbom: {
        packages: [
          { name: 'left-pad', version: '1.3.0', license: 'MIT', type: 'npm' },
          { name: 'lodash', version: '4.17.21', license: 'MIT', type: 'npm' },
          { name: 'react', version: '19.0.0', license: 'BSD-3-Clause', type: 'npm' },
        ],
      },
    })
  }))
  const { user } = show({ kind: 'document', title: 'sbom', source: { resource: 'sbom', extraIdentity: { arch: 'amd64' } } })
  expect(await screen.findByText('3 packages · 2 licenses · 1 ecosystem')).toBeInTheDocument()
  expect(asParam).toBe('document')
  expect(bodyRows('Packages')).toHaveLength(3)
  await user.type(screen.getByRole('textbox', { name: 'Filter packages' }), 'left')
  expect(bodyRows('Packages')).toHaveLength(1)
  expect(screen.getByRole('cell', { name: 'left-pad' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Download' })).toHaveAttribute('href', '/v1/components/acme.org%2Fapp/resources/1.0.0/sbom?extraIdentity.arch=amd64&repository=oci%3A%3Aghcr.io%2Facme')
  expect(screen.queryByRole('link', { name: 'Raw document' })).not.toBeInTheDocument()
  expect(screen.getByRole('dialog', { name: 'sbom' })).toBeInTheDocument()
})

test('vex viewer shows status counts without colour', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () =>
    HttpResponse.json({
      format: 'OpenVEX',
      vex: {
        statements: [
          { vulnerability: 'CVE-2024-2', products: ['pkg:oci/app'], status: 'affected', action: 'upgrade' },
          { vulnerability: 'CVE-2024-1', products: ['pkg:oci/app'], status: 'not_affected', justification: 'component_not_present' },
          { vulnerability: 'CVE-2024-3', products: ['pkg:oci/app'], status: 'not_affected', justification: 'inline_mitigations_already_exist' },
        ],
      },
    })))
  show({ kind: 'document', title: 'vex', source: { attestation: 'sha256:abc' }, originalUrl: 'https://example.com/vex.json' })
  expect(await screen.findByText('affected 1 · not_affected 2')).toBeInTheDocument()
  expect(bodyRows('Statements')).toHaveLength(3)
  expect(screen.getByRole('cell', { name: 'upgrade' })).toBeInTheDocument()
  const dialog = screen.getByRole('dialog', { name: 'vex' })
  for (const el of [dialog, ...dialog.querySelectorAll('*')]) expect(el.getAttribute('class') ?? '').not.toMatch(/green|emerald|red/)
  expect(screen.getByRole('link', { name: 'Raw document' })).toHaveAttribute('href', '/v1/components/acme.org%2Fapp/attestations/1.0.0/sha256%3Aabc?repository=oci%3A%3Aghcr.io%2Facme')
  expect(screen.getByRole('link', { name: 'Original document' })).toHaveAttribute('href', 'https://example.com/vex.json')
})

test('scan viewer shows severity counts and filters', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () =>
    HttpResponse.json({
      format: 'SARIF 2.1.0',
      scan: {
        tool: 'Trivy',
        importantBasis: 'security-severity',
        findings: [
          { rule: 'CVE-2024-44', severity: 'low', location: 'go.mod', message: 'old' },
          { rule: 'CVE-2024-1', severity: 'critical', location: 'go.sum', message: 'very bad' },
          { rule: 'CVE-2024-22', severity: 'high', location: 'go.mod', message: 'bad' },
          { rule: 'CVE-2024-33', severity: 'low', message: 'meh' },
        ],
      },
    })))
  const { user } = show({ kind: 'document', title: 'scan', source: { attestation: 'sha256:abc' } })
  expect(await screen.findByText('Trivy · 4 findings')).toBeInTheDocument()
  expect(screen.getByText('critical 1 · high 1 · low 2')).toBeInTheDocument()
  expect(bodyRows('Findings')).toHaveLength(4)
  await user.type(screen.getByRole('textbox', { name: 'Filter findings' }), 'CVE-2024-1')
  expect(bodyRows('Findings')).toHaveLength(1)
  expect(screen.getByRole('cell', { name: 'very bad' })).toBeInTheDocument()
})

test('a document with several parts switches between them, starting at the requested one', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () =>
    HttpResponse.json({
      format: 'CycloneDX 1.6',
      sbom: { packages: [{ name: 'libexpat1', version: '2.8.3' }] },
      vex: { statements: [{ vulnerability: 'CVE-2026-1', products: ['libexpat1'], status: 'affected', action: 'will_not_fix: no fix yet' }] },
      scan: { tool: '', importantBasis: 'level', findings: [{ rule: 'CVE-2026-4', message: 'open' }] },
    })))
  const { user } = show({ kind: 'document', part: 'vex', title: 'VEX', source: { attestation: 'sha256:abc' } })
  expect(await screen.findByRole('tab', { name: 'VEX', selected: true })).toBeInTheDocument()
  expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual(['Packages', 'VEX', 'Scan results'])
  expect(screen.getByRole('cell', { name: 'will_not_fix: no fix yet' })).toBeInTheDocument()
  await user.click(screen.getByRole('tab', { name: 'Scan results' }))
  expect(screen.getByText('1 findings')).toBeInTheDocument()
  await user.click(screen.getByRole('tab', { name: 'Packages' }))
  expect(bodyRows('Packages')).toHaveLength(1)
})

test('a single-part document shows no part switch', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () =>
    HttpResponse.json({ format: 'OpenVEX', vex: { statements: [] } })))
  show({ kind: 'document', part: 'sbom', title: 'VEX', source: { attestation: 'sha256:abc' } })
  expect(await screen.findByRole('table', { name: 'Statements' })).toBeInTheDocument()
  expect(screen.queryByRole('tablist')).not.toBeInTheDocument()
})

test('raw viewer pretty-prints the stored document and links it', async () => {
  let asParam: string | null = ''
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', ({ request }) => {
    asParam = new URL(request.url).searchParams.get('as')
    return HttpResponse.json({ buildDefinition: { buildType: 'https://actions.github.io/buildtypes/workflow/v1' }, runDetails: { builder: { id: 'https://github.com/actions/runner' } } })
  }))
  show({ kind: 'raw', title: 'SLSA provenance', attestation: 'sha256:abc' })
  const doc = await screen.findByLabelText('Document')
  expect(asParam).toBeNull()
  expect(doc.textContent).toContain('"id": "https://github.com/actions/runner"')
  expect(screen.getByRole('link', { name: 'Raw document' })).toHaveAttribute('href', '/v1/components/acme.org%2Fapp/attestations/1.0.0/sha256%3Aabc?repository=oci%3A%3Aghcr.io%2Facme')
})

test('raw viewer falls back to the raw link for a too large document', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () => documentError('DOCUMENT_TOO_LARGE', 422)))
  show({ kind: 'raw', title: 'SLSA provenance', attestation: 'sha256:abc' })
  expect(await screen.findByText("This document can't be shown here.")).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Raw document' })).toBeInTheDocument()
})

test('image viewer shows docker-inspect fields and raw links', async () => {
  let path = ''
  server.use(http.get('/v1/components/:name/images/:version/:resource/:digest', ({ request }) => {
    path = new URL(request.url).pathname
    return HttpResponse.json({
      digest: 'sha256:def',
      platform: 'linux/amd64',
      size: 12_500_000,
      created: '2026-01-02T03:04:05Z',
      user: '65532',
      workingDir: '/app',
      entrypoint: ['/app/server'],
      cmd: ['--port', '8080'],
      exposedPorts: ['8080/tcp'],
      env: ['PATH=/usr/bin', 'TZ=UTC', 'TZ=UTC'],
      labels: { 'org.opencontainers.image.source': 'https://github.com/acme/app' },
      layers: [
        { digest: 'sha256:l1', size: 2_000_000, createdBy: 'ADD rootfs /' },
        { digest: 'sha256:l2', size: 10_000_000, createdBy: 'COPY server /app/server' },
      ],
    })
  }))
  const error = vi.spyOn(console, 'error').mockImplementation(() => {})
  show({ kind: 'image', title: 'image', resource: 'image', extraIdentity: null, digest: 'sha256:def' })
  expect(await screen.findByText('linux/amd64')).toBeInTheDocument()
  expect(path).toBe('/v1/components/acme.org%2Fapp/images/1.0.0/image/sha256%3Adef')
  const field = (term: string) => screen.getByText(term, { selector: 'dt' }).nextElementSibling?.textContent
  expect(field('compressed size')).toBe('13 MB')
  expect(field('layers')).toBe('2')
  expect(field('created')).toBe('2026-01-02T03:04:05Z')
  expect(field('user')).toBe('65532')
  expect(field('entrypoint')).toBe('/app/server')
  expect(field('cmd')).toBe('--port 8080')
  expect(field('workdir')).toBe('/app')
  expect(field('exposed ports')).toBe('8080/tcp')
  expect(field('env')).toContain('TZ=UTC')
  expect(field('env')?.match(/TZ=UTC/g)).toHaveLength(2)
  expect(error).not.toHaveBeenCalled()
  error.mockRestore()
  expect(field('image labels')).toContain('org.opencontainers.image.source=https://github.com/acme/app')
  expect(bodyRows('Layers')).toHaveLength(2)
  expect(screen.getByRole('cell', { name: 'COPY server /app/server' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Raw manifest' })).toHaveAttribute('href', '/v1/components/acme.org%2Fapp/images/1.0.0/image/sha256%3Adef?repository=oci%3A%3Aghcr.io%2Facme&raw=manifest')
  expect(screen.getByRole('link', { name: 'Raw config' })).toHaveAttribute('href', '/v1/components/acme.org%2Fapp/images/1.0.0/image/sha256%3Adef?repository=oci%3A%3Aghcr.io%2Facme&raw=config')
})

test('unsupported document shows download only', async () => {
  server.use(http.get('/v1/components/:name/resources/:version/:resource', () => documentError('UNSUPPORTED_DOCUMENT', 422)))
  show({ kind: 'document', title: 'sbom', source: { resource: 'sbom' } })
  expect(await screen.findByText("This document can't be shown here.")).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Download' })).toBeInTheDocument()
  expect(screen.queryByRole('link', { name: 'Original document' })).not.toBeInTheDocument()
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})

test('changed document shows original link', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () =>
    documentError('DOCUMENT_CHANGED', 409, { url: 'https://example.com/now.json' })))
  show({ kind: 'document', title: 'vex', source: { attestation: 'sha256:abc' } })
  expect(await screen.findByText('The linked document changed since the last sync.')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Original document' })).toHaveAttribute('href', 'https://example.com/now.json')
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
})

test('a document off the allow list shows only the original link', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () =>
    documentError('DOCUMENT_NOT_ALLOWED', 403, { url: 'https://example.com/vex.json' })))
  show({ kind: 'document', title: 'vex', source: { attestation: 'sha256:abc' }, originalUrl: 'https://example.com/vex.json' })
  expect(await screen.findByText("This document can't be shown here.")).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Original document' })).toHaveAttribute('href', 'https://example.com/vex.json')
  expect(screen.queryByRole('link', { name: 'Raw document' })).not.toBeInTheDocument()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})

test('an original document URL that is not https is shown as text, not linked', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () =>
    documentError('DOCUMENT_CHANGED', 409, { url: 'javascript:alert(1)' })))
  show({ kind: 'document', title: 'vex', source: { attestation: 'sha256:abc' } })
  expect(await screen.findByText('javascript:alert(1)')).toBeInTheDocument()
  expect(screen.queryByRole('link', { name: 'Original document' })).not.toBeInTheDocument()
})

test('other errors show the error panel and the links', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () => documentError('UPSTREAM_ERROR', 502)))
  show({ kind: 'document', title: 'vex', source: { attestation: 'sha256:abc' } })
  expect(await screen.findByRole('alert')).toHaveTextContent('UPSTREAM_ERROR')
  expect(screen.getByRole('link', { name: 'Raw document' })).toBeInTheDocument()
})

test('json target opens a modal with pretty JSON', () => {
  show({ kind: 'json', title: 'label: config', value: { a: 1, b: ['x'] } })
  const dialog = screen.getByRole('dialog', { name: 'label: config' })
  expect(dialog).toHaveAttribute('aria-modal', 'true')
  expect(dialog.querySelector('pre')?.textContent).toBe(JSON.stringify({ a: 1, b: ['x'] }, null, 2))
})

test('Escape closes the panel', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () => delay('infinite')))
  const { user, onClose } = show({ kind: 'document', title: 'vex', source: { attestation: 'sha256:abc' } })
  await user.keyboard('{Escape}')
  expect(onClose).toHaveBeenCalledTimes(1)
  await user.click(screen.getByRole('button', { name: 'Close' }))
  expect(onClose).toHaveBeenCalledTimes(2)
})

test('shows Fetching document… while pending', async () => {
  server.use(http.get('/v1/components/:name/attestations/:version/:digest', () => delay('infinite')))
  show({ kind: 'document', title: 'sbom', source: { attestation: 'sha256:abc' } })
  expect(await screen.findByText('Fetching document…')).toBeInTheDocument()
})

test('more than 100 rows render through the virtual list', async () => {
  const packages = Array.from({ length: 150 }, (_, i) => ({ name: `pkg-${i}`, version: '1.0.0', license: 'MIT', type: 'npm' }))
  server.use(http.get('/v1/components/:name/resources/:version/:resource', () => HttpResponse.json({ format: 'SPDX 2.3', sbom: { packages } })))
  show({ kind: 'document', title: 'sbom', source: { resource: 'sbom' } })
  expect(await screen.findByText('150 packages · 1 license · 1 ecosystem')).toBeInTheDocument()
  const table = screen.getByRole('table', { name: 'Packages' })
  expect(table.tagName).toBe('DIV')
  expect(within(table).queryAllByRole('row').length).toBeLessThan(150)
})
