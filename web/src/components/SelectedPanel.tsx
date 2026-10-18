import { Fragment } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowUpRight } from 'lucide-react'
import { displayLocation, getComponent, getDescriptor } from '../api/client'
import { providerName } from '../api/ocm'
import { contentItems } from '../lib/contents'
import type { View } from '../lib/contentsRows'
import { relativeTime } from '../lib/time'
import { ContentsList } from './ContentsList'
import { ErrorPanel } from './ErrorPanel'

// repository pins the registry the descriptor is read from (the pinned root, or
// a reference resolved from another registry); otherwise the first location.
// openable offers to open the component as its own page (a selected
// reference the hub knows). view and onView select the contents view.
export function SelectedPanel({ name, version, repository, query, openable, view, onView }: { name: string; version: string; repository?: string; query?: string; openable: boolean; view: View; onView: (v: View) => void }) {
  const desc = useQuery({ queryKey: ['descriptor', name, version, repository ?? ''], queryFn: () => getDescriptor(name, version, repository) })
  const component = useQuery({ queryKey: ['component', name], queryFn: () => getComponent(name) })
  const repo = repository ?? component.data?.locations[0]?.repository
  const count = desc.data ? contentItems(desc.data).length : undefined
  const provider = desc.data ? providerName(desc.data) : ''
  const iso = desc.data?.component.creationTime
  const rel = iso ? relativeTime(iso) : undefined
  const subtitle = [
    provider && <span key="p">{`by ${provider}`}</span>,
    repo && <span key="r">{displayLocation(repo)}</span>,
    count !== undefined && <span key="n">{`${count} ${count === 1 ? 'item' : 'items'}`}</span>,
    iso && rel && <span key="c" title={iso}>{`Created ${rel}`}</span>,
  ].filter(Boolean)
  return (
    <section aria-label={`${name} ${version}`} className="min-w-0 flex-1 space-y-3">
      <div className="space-y-1 border-b border-slate-200 pb-2">
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate">
            <span className="font-semibold">{name}</span> <span className="text-slate-500">{version}</span>
          </span>
        </div>
        {openable && component.data && (
          <Link
            to="/components/$"
            params={{ _splat: name }}
            search={{ version: version === component.data.defaultVersion ? undefined : version }}
            aria-label={`Open ${name} ${version}`}
            className="inline-flex shrink-0 items-center gap-1 rounded-md border border-slate-300 bg-white px-2 py-0.5 text-sm text-slate-700 hover:border-blue-400 hover:text-blue-700"
          >
            Open
            <ArrowUpRight size={14} aria-hidden />
          </Link>
        )}
      </div>
      {subtitle.length > 0 && (
        <p className="flex flex-wrap gap-x-1 text-sm text-slate-500">
          {subtitle.map((part, i) => (
            <Fragment key={i}>
              {i > 0 && <span aria-hidden="true">·</span>}
              {part}
            </Fragment>
          ))}
        </p>
      )}
      </div>
      {desc.isError ? <ErrorPanel error={desc.error} /> : desc.data ? <ContentsList key={`${name}:${version}`} descriptor={desc.data} repository={repository} query={query} view={view} onView={onView} /> : <p className="text-sm text-slate-500">Loading…</p>}
    </section>
  )
}
