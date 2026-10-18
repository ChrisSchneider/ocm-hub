import { Award } from 'lucide-react'
import { Link } from '@tanstack/react-router'
import type { ComponentSummary } from '../api/client'
import { displayTitle, splitName } from '../lib/names'
import { ComponentIcon } from './ComponentIcon'
import { componentIcon } from '../lib/icon'

export function CatalogCard({ entry }: { entry: ComponentSummary }) {
  const n = entry.locations.length
  const { namespace } = splitName(entry.name)
  const meta = [entry.provider, entry.defaultVersion].filter(Boolean).join(' · ') + (n >= 2 ? ` · ${n} locations` : '')
  return (
    <Link
      to="/components/$"
      params={{ _splat: entry.name }}
      search={{}}
      className="flex flex-col gap-2 rounded-lg border border-slate-200 bg-white p-4 hover:shadow"
    >
      <div className="flex items-center gap-3">
        <ComponentIcon icon={componentIcon(entry)} title={displayTitle(entry.title, entry.name)} />
        <div className="min-w-0">
          <span className="block truncate font-semibold">{displayTitle(entry.title, entry.name)}</span>
          {namespace && <div className="truncate font-mono text-xs text-slate-500">{namespace}</div>}
          {meta && <div className="truncate text-xs text-slate-500">{meta}</div>}
        </div>
        <div className="ml-auto flex items-center gap-1">
          {!entry.latest && entry.defaultVersion && <span className="rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-600">pre-release</span>}
          {entry.signed && <span title="signed (not verified)">
              <Award size={16} className="text-slate-400" role="img" aria-label="signed (not verified)" />
            </span>}
        </div>
      </div>
      {entry.description && <p className="line-clamp-3 text-sm text-slate-600">{entry.description}</p>}
      {entry.categories && (
        <div className="flex flex-wrap gap-1">
          {entry.categories.map((c) => (
            <span key={c} className="rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-600">
              {c}
            </span>
          ))}
        </div>
      )}
    </Link>
  )
}
