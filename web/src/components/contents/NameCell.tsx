import { ComponentIcon, TypeIcon } from '../TypeIcon'

export const INDENT = ['', 'pl-5', 'pl-10'] as const

// NameCell: the first cell of every view, type icon and name, indented with └
// below depth 0. onToggle adds the "▸ N" toggle of an expandable row (N once count is known); onOpen
// makes the name a button (Contents: the element's YAML). type "component"
// is the component itself.
export function NameCell({ type, source, name, depth, count, expanded, onToggle, note, onOpen, open }: {
  type?: string
  source?: boolean
  name: string
  depth: 0 | 1 | 2
  count?: number
  expanded?: boolean
  onToggle?: () => void
  note?: string
  onOpen?: () => void
  open?: boolean
}) {
  const label = <span className="truncate font-medium" title={name}>{name}</span>
  return (
    <div role="cell" className={`flex min-w-0 items-center gap-2 ${INDENT[depth]}`}>
      {depth > 0 && (
        <span aria-hidden className="shrink-0 text-slate-400">
          └
        </span>
      )}
      {type === 'component' ? <ComponentIcon /> : <TypeIcon type={type} source={source} />}
      {onOpen ? (
        <button type="button" aria-label={`Details ${name}`} aria-expanded={!!open} onClick={onOpen} className="min-w-0 cursor-pointer truncate text-left">
          {label}
        </button>
      ) : (
        label
      )}
      {note && <span className="shrink-0 text-xs text-slate-500">{note}</span>}
      {onToggle && (
        <button
          type="button"
          aria-label={`Expand ${name}`}
          aria-expanded={!!expanded}
          onClick={onToggle}
          className="shrink-0 cursor-pointer rounded border border-slate-300 bg-white px-1 text-xs text-slate-700 hover:border-blue-400"
        >
          {expanded ? '▾' : '▸'}{count !== undefined && ` ${count}`}
        </button>
      )}
    </div>
  )
}
