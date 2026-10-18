import { ChevronDown } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { ConnectorFacet } from '../api/client'
import { SourceAvatar, TypeMark } from './SourceBadge'

function Checkbox({ checked, indeterminate, onChange }: { checked: boolean; indeterminate?: boolean; onChange: () => void }) {
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = !!indeterminate
  }, [indeterminate])
  return <input ref={ref} type="checkbox" checked={checked} onChange={onChange} />
}

// SourcesFacet lists the sources; a source with owner groups (an enterprise's
// orgs) expands to them. Selecting a source replaces its selected groups.
export function SourcesFacet({ values, selected, onChange }: { values: ConnectorFacet[]; selected: ReadonlySet<string>; onChange: (next: Set<string>) => void }) {
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set())
  if (values.length === 0) return null
  const toggleSource = (s: ConnectorFacet) => {
    const next = new Set(selected)
    for (const g of s.groups) next.delete(g.key)
    if (!next.delete(s.key)) next.add(s.key)
    onChange(next)
  }
  const toggleGroup = (s: ConnectorFacet, key: string) => {
    const next = new Set(selected)
    next.delete(s.key)
    if (!next.delete(key)) next.add(key)
    onChange(next)
  }
  const byType = new Map<string, ConnectorFacet[]>()
  for (const v of values) byType.set(v.type, [...(byType.get(v.type) ?? []), v])
  const toggleOpen = (key: string) => setOpen((o) => { const n = new Set(o); if (!n.delete(key)) n.add(key); return n })
  return (
    <fieldset className="mb-6">
      <legend className="mb-2 text-xs font-semibold uppercase text-slate-500">Sources</legend>
      {[...byType].map(([type, sources]) => (
        <div key={type} role="group" aria-label={typeLabel(type)} className="mb-2">
          <div className="mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-slate-600">
            <TypeMark type={type} />
            {typeLabel(type)}
          </div>
          {sources.map((s) => {
            const expanded = open.has(s.key)
            const some = s.groups.some((g) => selected.has(g.key))
            return (
              <div key={s.key}>
                <Row
                  title={s.title}
                  avatar={s}
                  count={s.count}
                  checkbox={<Checkbox checked={selected.has(s.key)} indeterminate={!selected.has(s.key) && some} onChange={() => toggleSource(s)} />}
                  expander={
                    s.groups.length > 0 && (
                      <button
                        type="button"
                        aria-expanded={expanded}
                        aria-label={`${expanded ? 'Hide' : 'Show'} orgs of ${s.title}`}
                        className="text-slate-500 hover:text-slate-900"
                        onClick={() => toggleOpen(s.key)}
                      >
                        <ChevronDown className={`size-3.5 transition-transform ${expanded ? '' : '-rotate-90'}`} />
                      </button>
                    )
                  }
                />
                {expanded && (
                  <div className="pl-5">
                    {s.groups.map((g) => (
                      <Row
                        key={g.key}
                        title={g.title}
                        avatar={{ title: g.title, type: s.type, avatarUrl: g.avatarUrl }}
                        count={g.count}
                        checkbox={<Checkbox checked={selected.has(g.key)} onChange={() => toggleGroup(s, g.key)} />}
                      />
                    ))}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      ))}
    </fieldset>
  )
}

// Row: checkbox, avatar and title, then count and the optional expander on the right.
function Row({ title, avatar, count, checkbox, expander }: { title: string; avatar: { title: string; type: string; avatarUrl?: string }; count: number; checkbox: React.ReactNode; expander?: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2 py-0.5 text-sm">
      <label className="flex min-w-0 flex-1 items-start gap-2">
        <span className="mt-0.5 flex">{checkbox}</span>
        <span className="mt-0.5 shrink-0">
          <SourceAvatar source={avatar} />
        </span>
        <span className="min-w-0 break-words">{title}</span>
      </label>
      <span className="shrink-0 text-slate-400">({count})</span>
      <span className="mt-0.5 flex w-3.5 shrink-0 justify-center">{expander}</span>
    </div>
  )
}

// typeLabel names a connector type for headings.
function typeLabel(type: string): string {
  return type.startsWith('github/') ? 'GitHub' : type
}
