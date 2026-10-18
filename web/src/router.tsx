import { createRootRoute, createRoute, createRouter, type RouterHistory } from '@tanstack/react-router'
import { Layout } from './components/Layout'
import { parseSearch, stringifySearch } from './lib/search'
import { CatalogPage } from './pages/CatalogPage'
import { ComponentPage } from './pages/ComponentPage'

export interface CatalogSearch {
  q?: string
  all?: true
}

// Absent values are the defaults: latest version, root selected, overview
// tab — so default URLs stay clean.
export interface ComponentSearch {
  version?: string
  select?: string
  // Page tab; absent: overview. labels, supply-chain and findings are Contents segments;
  // compliance (the old checklist tab) opens the overview.
  view?: 'versions' | 'contents' | 'labels' | 'descriptor' | 'supply-chain' | 'findings' | 'compliance' | 'usage' | 'docs' | 'replication'
  // Repository (location) the root component is read from; absent: the first location that has it.
  repository?: string
  // Registry a selected reference from another registry is read from.
  selectRepository?: string
  // In-component search (tree rows and contents).
  q?: string
}

const VIEWS = ['versions', 'contents', 'labels', 'descriptor', 'supply-chain', 'findings', 'compliance', 'usage', 'docs', 'replication'] as const

const str = (v: unknown): string | undefined => (v === undefined || v === null || v === '' ? undefined : String(v))

export const parseCatalogSearch = (s: Record<string, unknown>): CatalogSearch => ({
  q: str(s.q),
  all: s.all === true || s.all === '1' || s.all === 'true' ? true : undefined,
})

export const parseComponentSearch = (s: Record<string, unknown>): ComponentSearch => ({
  version: str(s.version),
  select: str(s.select),
  view: (VIEWS as readonly unknown[]).includes(s.view) ? (s.view as ComponentSearch['view']) : undefined,
  repository: str(s.repository),
  selectRepository: str(s.selectRepository),
  q: str(s.q),
})

const rootRoute = createRootRoute({ component: Layout })
const catalogRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', validateSearch: parseCatalogSearch, component: CatalogPage })
const componentRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/components/$',
  validateSearch: parseComponentSearch,
  component: ComponentPage,
})
const routeTree = rootRoute.addChildren([catalogRoute, componentRoute])

export function createAppRouter(history?: RouterHistory) {
  return createRouter({ routeTree, history, parseSearch, stringifySearch })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
