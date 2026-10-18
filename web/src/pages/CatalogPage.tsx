import { useInfiniteQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Plus, X } from 'lucide-react'
import { useState } from 'react'
import { listComponents, type ConnectorFacet } from '../api/client'
import { CatalogCard } from '../components/CatalogCard'
import { ErrorPanel } from '../components/ErrorPanel'
import { SourcesFacet } from '../components/SourcesFacet'
import { SidePanel } from '../components/viewers/SidePanel'

function toggle(set: ReadonlySet<string>, v: string): Set<string> {
  const next = new Set(set)
  if (!next.delete(v)) next.add(v)
  return next
}

interface FacetValue {
  value: string
  count: number
}

type Replication = 'single' | 'replicated'
const REPLICATION_LABEL: Record<Replication, string> = { single: 'Single location', replicated: 'Replicated (2+ locations)' }

function Facet({ title, values, selected, onToggle, label = (v) => v }: { title: string; values: FacetValue[]; selected: ReadonlySet<string>; onToggle: (v: string) => void; label?: (v: string) => string }) {
  if (values.length === 0) return null
  return (
    <fieldset className="mb-6">
      <legend className="mb-2 text-xs font-semibold uppercase text-slate-500">{title}</legend>
      {values.map(({ value, count }) => (
        <label key={value} className="flex items-center gap-2 py-0.5 text-sm">
          <input type="checkbox" checked={selected.has(value)} onChange={() => onToggle(value)} />
          <span className="min-w-0 break-words">{label(value)}</span> <span className="text-slate-400">({count})</span>
        </label>
      ))}
    </fieldset>
  )
}

// sourceTitle names a selected source or owner key from the facet.
function sourceTitle(facets: ConnectorFacet[], key: string): string {
  for (const s of facets) {
    if (s.key === key) return s.title
    const g = s.groups.find((g) => g.key === key)
    if (g) return g.title
  }
  return key
}

// Chip: an active filter with a button removing it.
function Chip({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-slate-300 bg-white py-0.5 pr-1 pl-2.5 text-sm">
      {label}
      <button type="button" aria-label={`Remove filter ${label}`} onClick={onRemove} className="rounded-full p-0.5 text-slate-500 hover:bg-slate-100 hover:text-slate-900">
        <X size={12} />
      </button>
    </span>
  )
}

export function CatalogPage() {
  const { q, all } = useSearch({ from: '/' })
  const navigate = useNavigate({ from: '/' })
  const topLevel = !q && !all
  const [sources, setSources] = useState<ReadonlySet<string>>(new Set())
  const [categories, setCategories] = useState<ReadonlySet<string>>(new Set())
  const [architectures, setArchitectures] = useState<ReadonlySet<string>>(new Set())
  const [providers, setProviders] = useState<ReadonlySet<string>>(new Set())
  const provider = [...providers][0] // single-select: at most one
  const [replications, setReplications] = useState<ReadonlySet<string>>(new Set())
  const replication = [...replications][0] as Replication | undefined // single-select
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [sort, setSort] = useState<'relevance' | 'updated' | 'name'>('relevance')
  const filtered = !!q || sources.size > 0 || categories.size > 0 || architectures.size > 0 || providers.size > 0 || replications.size > 0
  const list = useInfiniteQuery({
    queryKey: ['components', q ?? '', topLevel, [...sources], [...categories], [...architectures], [...providers], replication, sort],
    queryFn: ({ pageParam }) =>
      listComponents({ q, connector: [...sources], category: [...categories], architecture: [...architectures], provider, replication, topLevel: topLevel || undefined, includePrerelease: true, sort: sort === 'relevance' ? undefined : sort, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (p) => p.next,
    placeholderData: (prev) => prev, // keep the cards while a filter reloads
  })

  if (list.isPending) return <div className="h-40 animate-pulse rounded-lg bg-slate-100" aria-label="Loading" />
  if (list.isError) return <ErrorPanel error={list.error} />
  const entries = list.data.pages.flatMap((p) => p.components)
  const facets = list.data.pages[0].facets
  const chips = [
    ...[...sources].map((k) => ({ key: `s:${k}`, label: sourceTitle(facets.connectors, k), remove: () => setSources((s) => toggle(s, k)) })),
    ...[...replications].map((v) => ({ key: `r:${v}`, label: REPLICATION_LABEL[v as Replication] ?? v, remove: () => setReplications(new Set()) })),
    ...[...categories].map((v) => ({ key: `c:${v}`, label: v, remove: () => setCategories((s) => toggle(s, v)) })),
    ...[...architectures].map((v) => ({ key: `a:${v}`, label: v, remove: () => setArchitectures((s) => toggle(s, v)) })),
    ...[...providers].map((v) => ({ key: `p:${v}`, label: v, remove: () => setProviders(new Set()) })),
  ]
  const clearAll = () => {
    setSources(new Set())
    setReplications(new Set())
    setCategories(new Set())
    setArchitectures(new Set())
    setProviders(new Set())
  }
  const total = list.data.pages[0].total
  return (
    <div>
      {filtersOpen && (
        <SidePanel title="Filters" side="left" onClose={() => setFiltersOpen(false)}>
          <SourcesFacet values={facets.connectors} selected={sources} onChange={setSources} />
          <Facet title="Replication" values={facets.replication} selected={replications} label={(v) => REPLICATION_LABEL[v as Replication] ?? v} onToggle={(v) => setReplications((s) => (s.has(v) ? new Set() : new Set([v])))} />
          <Facet title="Categories" values={facets.categories} selected={categories} onToggle={(v) => setCategories((s) => toggle(s, v))} />
          <Facet title="Architecture" values={facets.architectures} selected={architectures} onToggle={(v) => setArchitectures((s) => toggle(s, v))} />
          <Facet title="Provider" values={facets.providers} selected={providers} onToggle={(v) => setProviders((s) => (s.has(v) ? new Set() : new Set([v])))} />
        </SidePanel>
      )}
      <section>
        <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-slate-600">
          <label className="flex items-center gap-2">
            Sort
            <select value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} className="rounded border border-slate-300 bg-white px-2 py-1 text-sm">
              <option value="relevance">Relevance</option>
              <option value="updated">Recently updated</option>
              <option value="name">Name</option>
            </select>
          </label>
          {!q && (
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={!!all} onChange={(e) => void navigate({ search: (s) => ({ ...s, all: e.target.checked ? true : undefined }) })} />
              Include sub-components
            </label>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              aria-expanded={filtersOpen}
              onClick={() => setFiltersOpen((o) => !o)}
              className="inline-flex items-center gap-1 rounded-md border border-slate-300 bg-white px-2.5 py-1 text-sm text-slate-700 hover:bg-slate-100"
            >
              <Plus size={14} /> Filter
            </button>
            {chips.map((c) => (
              <Chip key={c.key} label={c.label} onRemove={c.remove} />
            ))}
            {chips.length > 1 && (
              <button type="button" onClick={clearAll} className="text-sm text-slate-500 underline hover:text-slate-900">
                Clear all
              </button>
            )}
          </div>
          <span className="ml-auto text-slate-500">
            {total} {total === 1 ? 'component' : 'components'}
          </span>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {entries.map((e) => (
            <CatalogCard key={e.name} entry={e} />
          ))}
          {entries.length === 0 &&
            (!filtered && !topLevel ? (
              <p className="text-slate-600">No components indexed yet. Run <code>go run ./cmd/sync --config …</code>.</p>
            ) : (
              <p className="text-slate-500">No component matches.</p>
            ))}
        </div>
        {list.hasNextPage && (
          <button
            type="button"
            disabled={list.isFetchingNextPage}
            onClick={() => void list.fetchNextPage()}
            className="mt-4 rounded-md border border-slate-300 px-3 py-1 text-sm hover:bg-slate-100 disabled:opacity-50"
          >
            Load more
          </button>
        )}
      </section>
    </div>
  )
}
