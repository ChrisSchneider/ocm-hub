import { useNavigate } from '@tanstack/react-router'
import type { ComponentSearch } from '../router'

type View = NonNullable<ComponentSearch['view']> | 'overview'
const TABS: { id: View; label: string }[] = [
  { id: 'overview', label: 'Overview' },
  { id: 'docs', label: 'Docs' },
  { id: 'usage', label: 'Usage' },
  { id: 'versions', label: 'Versions' },
  { id: 'replication', label: 'Replication' },
  { id: 'contents', label: 'Contents' },
  { id: 'descriptor', label: 'Descriptor' },
]

// Page tabs of a component: the active one lives in ?view= (absent: overview); the Contents
// segments mark Contents, compliance marks Overview. noDocs greys out Docs (it still opens).
export function PageTabs({ view, noDocs }: { view: ComponentSearch['view']; noDocs?: boolean }) {
  const navigate = useNavigate({ from: '/components/$' })
  const active: View = view === 'labels' || view === 'attestations' ? 'contents' : view === undefined || view === 'compliance' ? 'overview' : view
  return (
    <div role="tablist" aria-label="Component views" className="flex gap-1 border-b border-slate-200">
      {TABS.map((t) => {
        const grey = noDocs && t.id === 'docs'
        const look = active === t.id ? 'border-blue-700 font-semibold text-slate-900' : grey ? 'border-transparent text-slate-400 hover:text-slate-600' : 'border-transparent text-slate-600 hover:text-slate-900'
        return (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={active === t.id}
            title={grey ? 'No documentation' : undefined}
            onClick={() => void navigate({ search: (s) => ({ ...s, view: t.id === 'overview' ? undefined : t.id }) })}
            className={`-mb-px border-b-2 px-3 py-1.5 text-sm ${look}`}
          >
            {t.label}
          </button>
        )
      })}
    </div>
  )
}
