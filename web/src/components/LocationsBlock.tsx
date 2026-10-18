import { useNavigate } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { displayLocation, type Component } from '../api/client'
import { plural } from '../lib/facts'
import { SourceAvatar, TypeMark } from './SourceBadge'
import { inspectSearch, useDefaultView } from './InspectLabel'

export function LocationRow({ l, component }: { l: Component['locations'][number]; component: Component }) {
  const navigate = useNavigate({ from: '/components/$' })
  const version = l.latest ?? component.defaultVersion
  const dflt = useDefaultView(component.name, component.defaultVersion)
  return (
    <tr>
      <td className="py-0.5 pl-5 pr-3 font-mono text-xs text-slate-600 break-all">{displayLocation(l.repository)}</td>
      <td className="px-3 font-mono text-xs">{l.latest || '—'}</td>
      <td className="px-3 text-xs text-slate-500">{plural(l.versionCount, 'version', 'versions')}</td>
      <td className="w-0 pr-1">
        <button
          type="button"
          aria-label={`Inspect ${version} at ${displayLocation(l.repository)}`}
          title={`Inspect ${version} at this location`}
          className="rounded p-0.5 text-slate-500 hover:bg-slate-200 hover:text-blue-700"
          onClick={() => void navigate({ search: (s) => ({ ...s, ...inspectSearch(version, l.repository, dflt) }) })}
        >
          <ArrowRight size={14} aria-hidden />
        </button>
      </td>
    </tr>
  )
}

// LocationsBlock lists every location of the component, grouped by source.
export function LocationsBlock({ component }: { component: Component }) {
  return (
    <section aria-labelledby="locations-heading" className="rounded-lg border border-slate-200 bg-white p-3">
      <h2 id="locations-heading" className="mb-2 text-sm font-semibold">Locations</h2>
      <table className="w-full text-left">
        {sourceEntries(component).map((e) => (
          <tbody key={e.key}>
            <tr>
              <th scope="colgroup" colSpan={4} className="pt-2 text-left text-sm font-medium">
                <span className="flex items-center gap-1.5">
                  <TypeMark type={e.type} />
                  <SourceAvatar source={e} />
                  {e.title}
                </span>
                {e.partOf && <span className="block pl-5 text-xs font-normal text-slate-500">Part of: {e.partOf}</span>}
              </th>
            </tr>
            {e.locations.flatMap((loc) => component.locations.filter((l) => l.repository === loc)).map((l) => (
              <LocationRow key={l.repository} l={l} component={component} />
            ))}
          </tbody>
        ))}
      </table>
    </section>
  )
}

export interface SourceEntry {
  key: string
  title: string
  type: string
  avatarUrl?: string
  partOf?: string // enterprise the org belongs to
  locations: string[]
}

// sourceEntries flattens the component's sources: an enterprise contributes its
// orgs (with "part of"), plus itself for locations no org holds; an org found
// by several sources appears once.
export function sourceEntries(component: Component): SourceEntry[] {
  const out = new Map<string, SourceEntry>()
  const add = (e: SourceEntry) => {
    const prev = out.get(e.key)
    if (!prev) return void out.set(e.key, e)
    prev.locations = [...new Set([...prev.locations, ...e.locations])]
    prev.partOf ??= e.partOf
  }
  for (const s of component.sources) {
    const rest = s.locations.filter((l) => !s.groups.some((g) => g.locations.includes(l)))
    if (rest.length > 0 || s.groups.length === 0) add({ key: s.key, title: s.title, type: s.type, avatarUrl: s.avatarUrl, locations: rest })
    for (const g of s.groups) add({ key: g.key, title: g.title, type: s.type, avatarUrl: g.avatarUrl, partOf: s.title, locations: g.locations })
  }
  return [...out.values()]
}
