import { Link, Outlet, useMatch, useNavigate, useRouterState, useSearch } from '@tanstack/react-router'
import { useEffect, useState, type FormEvent } from 'react'
import { ComponentTopBar } from './ComponentTopBar'

// Top bar of the catalog page: the catalog search. Typing filters the catalog
// (?q=); Enter with a component name (contains '/') opens that component.
function CatalogTopBar() {
  const search = useSearch({ strict: false }) as { q?: string }
  const navigate = useNavigate()
  const onCatalog = useRouterState({ select: (st) => st.location.pathname === '/' })
  const [value, setValue] = useState(search.q ?? '')

  // On the catalog the field mirrors ?q= (Link to "/", back/forward).
  const urlQ = onCatalog ? (search.q ?? '') : undefined
  useEffect(() => {
    if (urlQ !== undefined) setValue(urlQ)
  }, [urlQ])

  const onChange = (q: string) => {
    setValue(q)
    if (onCatalog) void navigate({ to: '/', search: { q: q || undefined }, replace: true })
  }
  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    const name = value.trim()
    if (name.includes('/')) void navigate({ to: '/components/$', params: { _splat: name } })
    else void navigate({ to: '/', search: { q: name || undefined } })
  }

  return (
    <header className="border-b border-slate-200 bg-white">
      <div className="mx-auto flex max-w-7xl items-center gap-3 px-4 py-3">
        <Link to="/" search={{}} className="font-semibold text-slate-900">
          OCM Hub
        </Link>
        <form onSubmit={onSubmit} className="ml-auto" role="search">
          <input
            type="search"
            aria-label="Search the catalog or open a component by name"
            placeholder="Search or open by name…"
            title="Search the catalog, or open a component by name (ocm.software/ocmcli)"
            value={value}
            onChange={(e) => onChange(e.target.value)}
            className="w-64 rounded-md border border-slate-300 px-3 py-1.5 text-sm"
          />
        </form>
      </div>
    </header>
  )
}

export function Layout() {
  const onComponent = useMatch({ from: '/components/$', shouldThrow: false })
  return (
    <div className="min-h-screen">
      {onComponent ? <ComponentTopBar /> : <CatalogTopBar />}
      <main className="mx-auto max-w-7xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}
