import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { getComponent } from '../api/client'
import { displayTitle, splitName } from '../lib/names'
import { ComponentIcon } from './ComponentIcon'
import { VersionTag, isDefaultView, useDefaultView } from './InspectLabel'
import { overviewQuery } from './useOverview'
import { componentIcon } from '../lib/icon'

// Top bar of a component page: breadcrumb with the component's title (the
// page's h1), the selected version with its location, and the in-component
// search (?q=; typing switches to the Contents view).
export function ComponentTopBar() {
  const { _splat: name = '' } = useParams({ from: '/components/$' })
  const search = useSearch({ from: '/components/$' })
  const navigate = useNavigate({ from: '/components/$' })
  const component = useQuery({ queryKey: ['component', name], queryFn: () => getComponent(name) })
  const [value, setValue] = useState(search.q ?? '')
  const urlQ = search.q ?? ''
  useEffect(() => setValue(urlQ), [urlQ]) // back/forward, links
  const o = component.data
  const version = search.version ?? o?.defaultVersion ?? ''
  // same key as the page and its tabs: no extra request
  const ov = useQuery({ ...overviewQuery(name, version, search.repository), enabled: !!o })
  const dflt = useDefaultView(name, o?.defaultVersion)
  const title = displayTitle(o?.title, name)
  const { namespace } = splitName(name)

  return (
    <header className="border-b border-slate-200 bg-white">
      <div className="mx-auto flex max-w-7xl items-center gap-3 px-4 py-3">
        <Link to="/" search={{}} className="font-semibold text-slate-900">
          OCM Hub
        </Link>
        <span aria-hidden className="text-slate-400">
          ›
        </span>
        {o && <ComponentIcon icon={componentIcon(o)} title={title} size={24} />}
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold">{title}</h1>
          {namespace && <div className="truncate font-mono text-xs text-slate-500">{namespace}</div>}
        </div>
        {o && (
          <div className="ml-auto flex items-center gap-3">
            <VersionTag version={version} repository={search.repository ?? ov.data?.repository} isDefault={isDefaultView(version, search.repository, dflt)} />
            <input
              type="search"
              aria-label="Search in this component"
              placeholder="Search in this component…"
              value={value}
              onChange={(e) => {
                setValue(e.target.value)
                const q = e.target.value || undefined
                void navigate({ search: (s) => ({ ...s, q, view: q ? 'contents' : s.view }), replace: true })
              }}
              className="w-64 rounded-md border border-slate-300 px-3 py-1.5 text-sm"
            />
          </div>
        )}
      </div>
    </header>
  )
}
