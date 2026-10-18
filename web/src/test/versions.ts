import { http, HttpResponse } from 'msw'
import { server } from './server'

export const versionsFixture = (includePre: boolean) => ({
  name: 'acme.org/x',
  versions: [
    ...(includePre ? [{ version: '2.0.0-rc.1', publishedAt: '2026-01-03T00:00:00Z', stable: false, locations: ['oci::ghcr.io/acme'] }] : []),
    { version: '1.1.0', publishedAt: '2026-01-02T00:00:00Z', stable: true, locations: ['oci::ghcr.io/acme', 'oci::mirror.example/acme'] },
    { version: '1.0.0', publishedAt: '2026-01-01T00:00:00Z', stable: true, locations: ['oci::ghcr.io/acme'] },
  ],
})

export function serveVersions(
  componentOverrides: object = {},
  versionsFn: (u: URL) => object = (u) => versionsFixture(u.searchParams.get('includePrerelease') === 'true'),
) {
  const component = {
    name: 'acme.org/x', title: 'X', latest: '1.1.0', defaultVersion: '1.1.0', versionCount: 3, signed: false, referencedBy: [], referencedByCount: 0,
    links: { versions: '' },
    sources: [
      { key: 'github:org:acme', title: 'Acme', type: 'github/v1', avatarUrl: 'https://avatars.example/acme', locations: ['oci::ghcr.io/acme'], groups: [] },
      { key: 'github:enterprise:acme', title: 'Acme Enterprise', type: 'github/v1', avatarUrl: 'https://avatars.example/ent', locations: ['oci::ghcr.io/acme', 'oci::mirror.example/acme'],
        groups: [{ key: 'github:org:mirror', title: 'Mirror Org', avatarUrl: 'https://avatars.example/mirror', locations: ['oci::mirror.example/acme'] }] },
    ],
    locations: [{ repository: 'oci::ghcr.io/acme', latest: '1.1.0', versionCount: 2 }, { repository: 'oci::mirror.example/acme', latest: '1.1.0', versionCount: 1 }],
    ...componentOverrides,
  }
  server.use(
    http.get('/v1/components/*', ({ request }) => {
      const u = new URL(request.url)
      if (u.pathname.includes('/overview/')) {
        return HttpResponse.json({ name: 'acme.org/x', version: '1.1.0', repository: 'oci::ghcr.io/acme', title: 'X', signed: false, responsibles: [], docs: [], contents: { resources: [], references: 0 }, resources: [], signatures: [] })
      }
      if (u.pathname.endsWith('/versions')) return HttpResponse.json(versionsFn(u))
      return HttpResponse.json(component)
    }),
  )
}
