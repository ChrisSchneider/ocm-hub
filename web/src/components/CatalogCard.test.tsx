import { render, screen } from '@testing-library/react'
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router'
import type { ComponentSummary } from '../api/client'
import { CatalogCard } from './CatalogCard'

function renderCard(entry: ComponentSummary) {
  const root = createRootRoute({ component: () => <CatalogCard entry={entry} /> })
  return render(<RouterProvider router={createRouter({ routeTree: root, history: createMemoryHistory() })} />)
}

const base: ComponentSummary = { name: 'ocm.software/plugins/ecrplugin', title: 'ocm.software/plugins/ecrplugin', provider: 'ocm.software', latest: '0.52.0', defaultVersion: '0.52.0', versionCount: 3, locations: ['oci::a'], signed: false }

test('title, namespace and meta line; one location is not mentioned', async () => {
  renderCard(base)
  expect(await screen.findByText('Ecrplugin')).toBeInTheDocument()
  expect(screen.getByText('ocm.software/plugins')).toBeInTheDocument()
  expect(screen.getByText('ocm.software · 0.52.0')).toBeInTheDocument()
  expect(screen.queryByText(/location/)).not.toBeInTheDocument()
  expect(screen.queryByText('Signed')).not.toBeInTheDocument()
})

test('a pre-release-only component shows its newest pre-release', async () => {
  renderCard({ ...base, latest: undefined, defaultVersion: '0.53.0-rc.1' })
  expect(await screen.findByText('ocm.software · 0.53.0-rc.1')).toBeInTheDocument()
  expect(screen.getByText('pre-release')).toBeInTheDocument()
})

test('two locations are counted and signed is marked', async () => {
  renderCard({ ...base, locations: ['oci::a', 'oci::b'], signed: true })
  expect(await screen.findByText('ocm.software · 0.52.0 · 2 locations')).toBeInTheDocument()
  expect(screen.getByRole('img', { name: 'signed (not verified)' })).toBeInTheDocument()
})

test('without an icon the card shows the hosting org avatar in a larger size; an own icon wins', () => {
  const { container, unmount } = render(<RouterProvider router={createRouter({ routeTree: createRootRoute({ component: () => <CatalogCard entry={{ ...base, ownerAvatarUrl: 'https://avatars.githubusercontent.com/u/1?s=32&v=4' }} /> }), history: createMemoryHistory() })} />)
  return screen.findByText('Ecrplugin').then(() => {
    expect(container.querySelector('img')).toHaveAttribute('src', 'https://avatars.githubusercontent.com/u/1?s=96&v=4')
    unmount()
    renderCard({ ...base, icon: 'https://example.com/icon.svg', ownerAvatarUrl: 'https://avatars.githubusercontent.com/u/1?s=32&v=4' })
    return screen.findByText('Ecrplugin').then(() => expect(document.querySelector('img')).toHaveAttribute('src', 'https://example.com/icon.svg'))
  })
})

test('signed card shows a grey certificate icon, no green', async () => {
  const { container } = renderCard({ ...base, signed: true })
  const icon = await screen.findByLabelText('signed (not verified)')
  expect(icon.closest('[title]')).toHaveAttribute('title', 'signed (not verified)')
  expect(icon.getAttribute('class')).toContain('text-slate-400')
  expect(container.innerHTML).not.toMatch(/emerald|green/)
  expect(screen.queryByText('Signed')).not.toBeInTheDocument()
})
