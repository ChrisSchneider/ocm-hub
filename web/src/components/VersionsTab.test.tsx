import { screen, waitFor, within } from '@testing-library/react'
import { renderApp } from '../test/render'
import { serveVersions, versionsFixture } from '../test/versions'

const search = (router: { state: { matches: { search: unknown }[] } }) => router.state.matches.at(-1)?.search

test('lists versions newest first with publish dates; clicking a row inspects it at its first location', async () => {
  serveVersions()
  const { user, router } = renderApp('/components/acme.org/x?view=versions')
  const rows = await screen.findAllByRole('listitem')
  expect(rows.map((r) => r.getAttribute('data-version'))).toEqual(['1.1.0', '1.0.0'])
  expect(within(rows[1]).getByText(/2026/)).toBeInTheDocument()
  await user.click(within(rows[1]).getByRole('button', { name: '1.0.0' }))
  await waitFor(() => expect(search(router)).toMatchObject({ view: 'versions', version: '1.0.0', repository: 'oci::ghcr.io/acme' }))
  expect(await screen.findByRole('listitem', { current: true })).toHaveAttribute('data-version', '1.0.0')
})

test('a location chip inspects the version at that location', async () => {
  serveVersions()
  const { user, router } = renderApp('/components/acme.org/x?view=versions')
  const row = (await screen.findAllByRole('listitem'))[0]
  await user.click(within(row).getByRole('button', { name: 'mirror.example/acme' }))
  await waitFor(() => expect(search(router)).toMatchObject({ version: '1.1.0', repository: 'oci::mirror.example/acme' }))
})

test('Browse contents opens the contents view for that version', async () => {
  serveVersions()
  const { user, router } = renderApp('/components/acme.org/x?view=versions')
  const rows = await screen.findAllByRole('listitem')
  await user.click(within(rows[1]).getByRole('button', { name: 'Browse contents' }))
  await waitFor(() => expect(search(router)).toMatchObject({ view: 'contents', version: '1.0.0', repository: 'oci::ghcr.io/acme' }))
})

test('pre-releases appear with the toggle and are tagged', async () => {
  serveVersions()
  const { user } = renderApp('/components/acme.org/x?view=versions')
  await screen.findAllByRole('listitem')
  await user.click(screen.getByRole('checkbox', { name: 'Show pre-releases' }))
  const rc = await screen.findByText('2.0.0-rc.1')
  expect(within(rc.closest('li')!).getByText('pre-release')).toBeInTheDocument()
})

test('a component with only pre-releases starts with pre-releases shown', async () => {
  serveVersions({ latest: undefined, defaultVersion: '2.0.0-rc.1' })
  renderApp('/components/acme.org/x?view=versions')
  expect(await screen.findByText('2.0.0-rc.1')).toBeInTheDocument()
  expect(screen.getByRole('checkbox', { name: 'Show pre-releases' })).toBeChecked()
})

test('no stable versions hints at the pre-release toggle', async () => {
  serveVersions({}, () => ({ name: 'acme.org/x', versions: [] }))
  renderApp('/components/acme.org/x?view=versions')
  expect(await screen.findByText(/No stable versions\. Turn on "Show pre-releases"/)).toBeInTheDocument()
})

test('Load more fetches the next page', async () => {
  const all = versionsFixture(false).versions
  serveVersions({}, (u) => (u.searchParams.get('cursor') ? { name: 'acme.org/x', versions: [all[1]] } : { name: 'acme.org/x', versions: [all[0]], next: 'c1' }))
  const { user } = renderApp('/components/acme.org/x?view=versions')
  expect(await screen.findAllByRole('listitem')).toHaveLength(1)
  await user.click(screen.getByRole('button', { name: 'Load more' }))
  await waitFor(() => expect(screen.getAllByRole('listitem').map((r) => r.getAttribute('data-version'))).toEqual(['1.1.0', '1.0.0']))
  expect(screen.queryByRole('button', { name: 'Load more' })).not.toBeInTheDocument()
})

test('a chip shows the org avatar for grouped locations and the source avatar otherwise', async () => {
  serveVersions()
  renderApp('/components/acme.org/x?view=versions')
  const row = (await screen.findAllByRole('listitem'))[0]
  const mirror = within(row).getByRole('button', { name: 'mirror.example/acme' })
  expect([...mirror.querySelectorAll('img')].map((i) => i.getAttribute('src'))).toEqual(['https://avatars.example/mirror'])
  const ghcr = within(row).getByRole('button', { name: 'ghcr.io/acme' })
  expect([...ghcr.querySelectorAll('img')].map((i) => i.getAttribute('src'))).toEqual(['https://avatars.example/acme', 'https://avatars.example/ent'])
})

test('a chip without any avatar renders no empty wrapper', async () => {
  serveVersions({ sources: [{ key: 'k', title: 'Plain', type: 'github/v1', locations: ['oci::ghcr.io/acme'], groups: [] }] })
  renderApp('/components/acme.org/x?view=versions')
  const row = (await screen.findAllByRole('listitem'))[0]
  const chip = within(row).getByRole('button', { name: 'ghcr.io/acme' })
  expect(chip.querySelectorAll('img')).toHaveLength(0)
  expect(chip.querySelector('span')).toBeNull()
})

test('a chip shows one avatar when an enterprise group and its own org entry share a key', async () => {
  serveVersions({
    sources: [
      { key: 'github:org:mirror', title: 'Mirror Org', type: 'github/v1', avatarUrl: 'https://avatars.example/mirror', locations: ['oci::ghcr.io/acme'], groups: [] },
      { key: 'github:enterprise:acme', title: 'Ent', type: 'github/v1', locations: ['oci::ghcr.io/acme'],
        groups: [{ key: 'github:org:mirror', title: 'Mirror Org', avatarUrl: 'https://avatars.example/mirror', locations: ['oci::ghcr.io/acme'] }] },
    ],
  })
  const error = vi.spyOn(console, 'error').mockImplementation(() => {})
  renderApp('/components/acme.org/x?view=versions')
  const row = (await screen.findAllByRole('listitem'))[0]
  const chip = within(row).getByRole('button', { name: 'ghcr.io/acme' })
  expect([...chip.querySelectorAll('img')].map((i) => i.getAttribute('src'))).toEqual(['https://avatars.example/mirror'])
  expect(error).not.toHaveBeenCalled()
  error.mockRestore()
})

test('picking the default version at its default location keeps the URL clean and the tag unhighlighted', async () => {
  serveVersions() // default 1.1.0; the default overview is read from oci::ghcr.io/acme
  const { user, router } = renderApp('/components/acme.org/x?view=versions')
  const tag = await screen.findByRole('status', { name: 'Selected version' })
  const row = (await screen.findAllByRole('listitem'))[0]
  await user.click(within(row).getByRole('button', { name: '1.1.0' }))
  await waitFor(() => expect(search(router)).toEqual({ view: 'versions' }))
  expect(tag).toHaveAttribute('data-default', 'true')
  expect(within(tag).queryByRole('button', { name: 'Switch to default' })).not.toBeInTheDocument()
  // the same version at another location is not the default
  await user.click(within(row).getByRole('button', { name: 'mirror.example/acme' }))
  await waitFor(() => expect(search(router)).toMatchObject({ version: '1.1.0', repository: 'oci::mirror.example/acme' }))
  await waitFor(() => expect(screen.getByRole('status', { name: 'Selected version' })).toHaveAttribute('data-default', 'false'))
})

test('a pinned URL of the default version at its default location shows the tag as default', async () => {
  serveVersions()
  renderApp('/components/acme.org/x?view=versions&version=1.1.0&repository=oci%3A%3Aghcr.io%2Facme')
  const tag = await screen.findByRole('status', { name: 'Selected version' })
  await waitFor(() => expect(tag).toHaveAttribute('data-default', 'true'))
})

test('the locations block moved from Versions to the Replication tab', async () => {
  serveVersions()
  const { user } = renderApp('/components/acme.org/x?view=versions')
  await screen.findAllByRole('listitem')
  expect(screen.queryByRole('region', { name: 'Locations' })).not.toBeInTheDocument()
  await user.click(screen.getByRole('tab', { name: 'Replication' }))
  expect(await screen.findByRole('region', { name: 'Locations' })).toBeInTheDocument()
})

test('locations block lists each location with latest and count', async () => {
  serveVersions()
  renderApp('/components/acme.org/x?view=replication')
  const block = await screen.findByRole('region', { name: 'Locations' })
  expect(within(block).getByRole('heading', { name: 'Locations' })).toBeInTheDocument()
  const ghcr = within(block).getAllByText('ghcr.io/acme')[0].closest('tr')!
  expect(within(ghcr).getByText('1.1.0')).toBeInTheDocument()
  expect(within(ghcr).getByText('2 versions')).toBeInTheDocument()
  const mirror = within(block).getAllByText('mirror.example/acme')[0].closest('tr')!
  expect(within(mirror).getByText('1 version')).toBeInTheDocument()
})

test('a location without a latest version shows —', async () => {
  serveVersions({ locations: [{ repository: 'oci::ghcr.io/acme', latest: '1.1.0', versionCount: 2 }, { repository: 'oci::mirror.example/acme', versionCount: 0 }] })
  renderApp('/components/acme.org/x?view=replication')
  const block = await screen.findByRole('region', { name: 'Locations' })
  const mirror = within(block).getAllByText('mirror.example/acme')[0].closest('tr')!
  expect(within(mirror).getAllByRole('cell')[1]).toHaveTextContent(/^—$/)
})

test('enterprise org shows part of', async () => {
  serveVersions()
  renderApp('/components/acme.org/x?view=replication')
  const block = await screen.findByRole('region', { name: 'Locations' })
  expect(within(block).getByText('Part of: Acme Enterprise')).toBeInTheDocument()
  expect(within(block).getByText('Mirror Org')).toBeInTheDocument()
})

test('inspect arrow navigates to that location', async () => {
  serveVersions()
  const { user, router } = renderApp('/components/acme.org/x?view=replication')
  const block = await screen.findByRole('region', { name: 'Locations' })
  await user.click(within(block).getByRole('button', { name: 'Inspect 1.1.0 at mirror.example/acme' }))
  await waitFor(() => expect(search(router)).toMatchObject({ version: '1.1.0', repository: 'oci::mirror.example/acme' }))
})

test('version without changelog shows No changelog', async () => {
  const all = versionsFixture(false).versions
  serveVersions({}, () => ({ name: 'acme.org/x', versions: [{ ...all[0], hasChangelog: false }, { ...all[1], hasChangelog: true }] }))
  renderApp('/components/acme.org/x?view=versions')
  const rows = await screen.findAllByRole('listitem')
  expect(within(rows[0]).getByText('No changelog')).toBeInTheDocument()
  expect(within(rows[0]).queryByRole('button', { name: 'Show changelog' })).not.toBeInTheDocument()
  expect(within(rows[1]).getByRole('button', { name: 'Show changelog' })).toBeInTheDocument()
})

test('version with unknown changelog keeps the toggle', async () => {
  serveVersions()
  renderApp('/components/acme.org/x?view=versions')
  const rows = await screen.findAllByRole('listitem')
  expect(within(rows[0]).getByRole('button', { name: 'Show changelog' })).toBeInTheDocument()
  expect(within(rows[0]).queryByText('No changelog')).not.toBeInTheDocument()
})
