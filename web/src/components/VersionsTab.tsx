import { useInfiniteQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { displayLocation, getVersions, type Component, type Source, type VersionEntry } from '../api/client'
import type { ComponentSearch } from '../router'
import { formatDate } from '../lib/time'
import { Changelogs } from './Changelogs'
import { ErrorPanel } from './ErrorPanel'
import { inspectSearch } from './InspectLabel'
import { SourceAvatar } from './SourceBadge'
import { useDefaultView, type DefaultView } from './InspectLabel'

export function VersionsTab({ name, component, search }: { name: string; component: Component; search: ComponentSearch }) {
  const [includePre, setIncludePre] = useState(!component.latest) // only pre-releases: show them
  const versions = useInfiniteQuery({
    queryKey: ['versions', name, '', includePre],
    queryFn: ({ pageParam }) => getVersions(name, { includePrerelease: includePre, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (p) => p.next,
  })
  const inspected = search.version ?? component.defaultVersion
  const dflt = useDefaultView(name, component.defaultVersion)
  const entries = versions.data?.pages.flatMap((p) => p.versions) ?? []
  return (
    <section aria-label="Versions" className="space-y-3">
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={includePre} onChange={(e) => setIncludePre(e.target.checked)} />
        Show pre-releases
      </label>
      {versions.isError && <ErrorPanel error={versions.error} />}
      {versions.isPending && <p className="text-sm text-slate-500">Loading…</p>}
      {versions.isSuccess && entries.length === 0 && (
        <p className="text-sm text-slate-600">{includePre ? 'No versions.' : 'No stable versions. Turn on "Show pre-releases" to see the others.'}</p>
      )}
      <ul className="divide-y divide-slate-200 rounded-lg border border-slate-200 bg-white">
        {entries.map((e) => (
          <VersionRow key={e.version} name={name} entry={e} current={e.version === inspected} search={search} sources={component.sources} dflt={dflt} />
        ))}
      </ul>
      {versions.hasNextPage && (
        <button type="button" disabled={versions.isFetchingNextPage} onClick={() => void versions.fetchNextPage()}
          className="rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-50">
          Load more
        </button>
      )}
    </section>
  )
}

export function VersionRow({ name, entry, current, search, sources, dflt }: { name: string; entry: VersionEntry; current: boolean; search: ComponentSearch; sources: Source[]; dflt?: DefaultView }) {
  const navigate = useNavigate({ from: '/components/$' })
  const [open, setOpen] = useState(false)
  const changelogRepo = current && search.repository && entry.locations.includes(search.repository) ? search.repository : entry.locations[0]
  const inspect = (repository: string, view = search.view) => void navigate({ search: (s) => ({ ...s, ...inspectSearch(entry.version, repository, dflt), view }) })
  return (
    <li data-version={entry.version} aria-current={current || undefined} className={`space-y-1 px-4 py-2 ${current ? 'bg-blue-50' : ''}`}>
      <div className="flex flex-wrap items-center gap-3">
        <button type="button" className="font-mono font-semibold hover:underline" onClick={() => inspect(entry.locations[0])}>
          {entry.version}
        </button>
        {!entry.stable && <span className="rounded bg-slate-100 px-1.5 text-xs text-slate-600">pre-release</span>}
        <span className="text-xs text-slate-500">{formatDate(entry.publishedAt)}</span>
        <span className="flex flex-wrap gap-1">
          {entry.locations.map((l) => (
            <LocationChip key={l} location={l} avatars={avatarsFor(sources, l)} onClick={() => inspect(l)} />
          ))}
        </span>
        <button type="button" className="ml-auto text-sm text-blue-700 underline" onClick={() => inspect(entry.locations[0], 'contents')}>
          Browse contents
        </button>
        {entry.hasChangelog === false ? (
          <span className="text-sm text-slate-400">No changelog</span>
        ) : (
          <button type="button" aria-expanded={open} className="text-sm text-blue-700 underline" onClick={() => setOpen(!open)}>
            {open ? 'Hide changelog' : 'Show changelog'}
          </button>
        )}
      </div>
      {open && <Changelogs name={name} version={entry.version} repository={changelogRepo} />}
    </li>
  )
}

type ChipAvatar = { key: string; title: string; type: string; avatarUrl?: string }

// avatarsFor: per source holding location l, its group's avatar if a group holds l, else the source's.
function avatarsFor(sources: Source[], l: string): ChipAvatar[] {
  const out = new Map<string, ChipAvatar>()
  for (const s of sources.filter((s) => s.locations.includes(l))) {
    const g = s.groups.find((g) => g.locations.includes(l))
    const a = g ? { key: g.key, title: g.title, avatarUrl: g.avatarUrl, type: s.type } : { key: s.key, title: s.title, avatarUrl: s.avatarUrl, type: s.type }
    if (!out.has(a.key)) out.set(a.key, a)
  }
  return [...out.values()]
}

function LocationChip({ location, avatars, onClick }: { location: string; avatars: ChipAvatar[]; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="inline-flex items-center gap-1 rounded border border-slate-200 px-1.5 font-mono text-xs text-slate-600 hover:bg-slate-100">
      {avatars.some((a) => a.avatarUrl) && (
        <span className="inline-flex -space-x-1">
          {avatars.map((a) => <SourceAvatar key={a.key} source={a} />)}
        </span>
      )}
      {displayLocation(location)}
    </button>
  )
}
