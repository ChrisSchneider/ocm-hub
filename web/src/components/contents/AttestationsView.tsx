import type { ReactNode } from 'react'
import { Award, Eye } from 'lucide-react'
import { ApiError } from '../../api/client'
import { attestationName, detailsText, entriesOf, entryViewer, isAttestation, levelAnchor, levelFor, type AttestationSet, type ResourceIdentity } from '../../lib/attestations'
import { isImage } from '../../lib/contentsRows'
import { NotCollected, subjectLabel } from '../NotCollected'
import { ErrorPanel } from '../ErrorPanel'
import { attestationTarget, type ViewerScope, type ViewerTarget } from '../viewers/ViewerPanel'
import { VirtualList } from '../VirtualList'
import { lineLabel, rowClass, type Line } from './grid'
import { ImageNameCell } from './ImageNameCell'
import { NameCell } from './NameCell'
import { platformName, PlatformRows } from './PlatformRows'
import { ScrollToHash } from './ScrollToHash'

// AttestationsView: name | attestation | predicate type | details | signed | 👁, one line per
// attestation. Until the set is loaded only the names show; its state is reported under the table.
export function AttestationsView({ lines, expanded, onToggle, showComponent, scope, set, pending, error, onViewer }: {
  lines: Line[]
  expanded: Set<string>
  onToggle: (key: string) => void
  showComponent: boolean
  scope: ViewerScope
  set?: AttestationSet
  pending: boolean
  error: Error | null
  onViewer: (t: ViewerTarget) => void
}) {
  const empty = <div role="cell" />
  const cls = (source: boolean, last: boolean) => `${rowClass('attestations', source)} py-1 ${last ? 'border-b border-slate-100' : ''}`
  // note: the text of an item without attestation lines ("—", "per resource", …).
  const single = (aria: string, anchor: string | undefined, nameCell: ReactNode, note: string, source = false, key?: string) => (
    <div key={key} role="row" aria-label={aria} id={anchor} className={cls(source, true)}>
      {nameCell}
      <div role="cell" className="text-xs text-slate-400">{note}</div>
      {empty}
      {empty}
      {empty}
      {empty}
    </div>
  )
  const subject = (key: string, aria: string, nameCell: ReactNode, id: ResourceIdentity | undefined, platform?: string) => {
    const anchor = platform === undefined ? levelAnchor(id) : undefined
    if (!set) return single(aria, anchor, nameCell, '', false, key)
    const level = levelFor(set, id)
    const entries = entriesOf(level, platform)
    if (entries.length === 0) return single(aria, anchor, nameCell, platform === undefined && level?.entries.some((e) => isAttestation(e) && e.platform) ? 'on platforms' : '—', false, key)
    return entries.map((e, i) => {
      const viewer = entryViewer(e)
      const title = attestationName(e)
      const details = detailsText(e)
      return (
        <div key={`${key}/${i}`} role="row" aria-label={i === 0 ? aria : undefined} id={i === 0 ? anchor : undefined} className={cls(false, i === entries.length - 1)}>
          {i === 0 ? nameCell : empty}
          <div role="cell" className="truncate text-xs font-semibold" title={title}>{title}</div>
          <div role="cell" className="truncate font-mono text-xs text-slate-500" title={e.type}>{e.type}</div>
          <div role="cell" className="truncate text-slate-600" title={details || undefined}>{details}</div>
          <div role="cell">
            {e.signed === 'signed' && (
              <span title="signed (not verified)" className="inline-flex text-slate-400">
                <Award aria-hidden size={16} />
              </span>
            )}
          </div>
          <div role="cell">
            {viewer && e.digest && (
              <button type="button" aria-label={`View ${title}`} onClick={() => onViewer(attestationTarget(viewer, title, e.digest!, e.url))} className="cursor-pointer text-slate-500 hover:text-blue-700">
                <Eye aria-hidden size={16} />
              </button>
            )}
          </div>
        </div>
      )
    })
  }
  const row = (l: Line) => {
    if (l.kind === 'group') {
      const g = l.group
      return single(lineLabel(l), undefined, <NameCell type={g.type} name={g.name} depth={0} count={g.children.length} expanded={expanded.has(l.key)} onToggle={() => onToggle(l.key)} />, set ? 'per resource' : '')
    }
    if (l.kind === 'platforms') {
      const el = l.item.element
      return (
        <PlatformRows
          scope={scope}
          line={l}
          view="attestations"
          render={(_, p) => subject(`${l.key}/${p.digest}`, `platform ${platformName(p.platform)}`, <NameCell name={platformName(p.platform)} depth={l.depth} />, { name: el.name, version: el.version, extraIdentity: el.extraIdentity ?? undefined }, p.platform)}
        />
      )
    }
    const { kind, element: el } = l.item
    const nameCell = kind === 'resource' && isImage(el.type)
      ? <ImageNameCell scope={scope} item={l.item} depth={l.depth} label={l.label} note={l.note} expanded={expanded.has(l.key)} onToggle={() => onToggle(l.key)} />
      : <NameCell type={el.type} source={kind === 'source'} name={l.label} depth={l.depth} note={l.note} />
    if (kind === 'source') return single(lineLabel(l), undefined, nameCell, set ? '—' : '', true)
    return subject(l.key, lineLabel(l), nameCell, { name: el.name, version: el.version, extraIdentity: el.extraIdentity ?? undefined })
  }
  const failed = set?.levels.filter((l) => l.status === 'failed') ?? []
  return (
    <>
      <ScrollToHash settled={!pending} />
      <div role="table" aria-label="Attestations" className="text-sm">
        <div role="row" className={`${rowClass('attestations')} border-b border-slate-300 py-1 text-xs font-semibold uppercase tracking-wide text-slate-500`}>
          <span role="columnheader">Name</span>
          <span role="columnheader">Attestation</span>
          <span role="columnheader">Predicate type</span>
          <span role="columnheader">Details</span>
          <span role="columnheader">Signed</span>
          <span role="columnheader" className="sr-only">View</span>
        </div>
        {showComponent && subject('component', 'component itself', <NameCell type="component" name="component itself" depth={0} />, undefined)}
        <VirtualList items={lines} getKey={(l) => l.key} renderRow={row} role="rowgroup" itemRole="presentation" />
      </div>
      {pending ? (
        <p className="text-sm text-slate-500">Loading attestations…</p>
      ) : error instanceof ApiError && error.code === 'NOT_COLLECTED' ? (
        <NotCollected name={scope.name} error={error} view="attestations" />
      ) : error ? (
        <ErrorPanel error={error} />
      ) : null}
      {failed.map((l, i) => (
        <p key={i} className="text-sm text-slate-600">
          Couldn&apos;t check {subjectLabel(l)}: {l.error}
        </p>
      ))}
    </>
  )
}
