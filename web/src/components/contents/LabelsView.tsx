import type { ReactNode } from 'react'
import { Eye, Maximize2 } from 'lucide-react'
import { attestationContentUrl } from '../../api/client'
import type { OcmLabel } from '../../api/ocm'
import { attestationName, entryViewer, isHttpsUrl, labelEntry, type AttestationSet, type Entry, type ResourceIdentity } from '../../lib/attestations'
import { labelValue, VEX_LABEL } from '../../lib/labels'
import { attestationTarget, type ViewerTarget } from '../viewers/ViewerPanel'
import { VirtualList } from '../VirtualList'
import { lineLabel, rowClass, type Line } from './grid'
import { NameCell } from './NameCell'

const iconButton = 'cursor-pointer text-slate-500 hover:text-blue-700'
const linkClass = 'text-blue-700 underline'

interface Props {
  lines: Line[]
  expanded: Set<string>
  onToggle: (key: string) => void
  // componentLabels: the component's own labels; undefined hides the component row.
  componentLabels?: OcmLabel[]
  name: string
  version: string
  repository?: string
  // set and settled: the attestation set, once its request finished (successfully or not).
  set?: AttestationSet
  settled: boolean
  loadSet: () => Promise<AttestationSet | undefined>
  onViewer: (t: ViewerTarget) => void
}

// VexActions: 👁 (the VEX part of the document viewer) and ⬇ of a VEX link label, through the
// label-link attestation of the same subject. The set is loaded on the first click; without a digest the label's URL is linked.
// Sources are no attestation subject (subject false).
function VexActions({ id, subject, label, p }: { id?: ResourceIdentity; subject: boolean; label: OcmLabel; p: Props }) {
  const e = subject ? labelEntry(p.set, id, VEX_LABEL) : undefined
  const contentUrl = (digest: string) => attestationContentUrl(p.name, p.version, digest, p.repository)
  if (!subject || (p.settled && !e?.digest)) {
    const url = e?.url ?? (typeof label.value === 'string' ? label.value : undefined)
    if (!url) return null
    return isHttpsUrl(url) ? (
      <a href={url} target="_blank" rel="noreferrer" className={`${linkClass} text-xs`}>
        Original document
      </a>
    ) : <span className="break-all text-xs text-slate-500">{url}</span>
  }
  const withEntry = async (act: (digest: string, found: Entry) => void) => {
    const found = labelEntry(p.set ?? (await p.loadSet()), id, VEX_LABEL)
    if (found?.digest) act(found.digest, found)
  }
  const view = () => void withEntry((digest, found) => p.onViewer(attestationTarget(entryViewer(found) ?? 'document', attestationName(found), digest, found.url, 'vex')))
  return (
    <>
      <button type="button" aria-label={`View ${label.name}`} onClick={view} className={iconButton}>
        <Eye aria-hidden size={16} />
      </button>
      {e?.digest ? (
        <a href={contentUrl(e.digest)} download aria-label={`Download ${label.name}`} className={iconButton}>
          ⬇
        </a>
      ) : (
        <button type="button" aria-label={`Download ${label.name}`} onClick={() => void withEntry((digest) => window.location.assign(contentUrl(digest)))} className={iconButton}>
          ⬇
        </button>
      )}
    </>
  )
}

// LabelsView: name | label | value | actions, one line per label; the name only on an item's first line.
// A JSON value shows as {…} and opens in full on click; only VEX labels have actions.
export function LabelsView(p: Props) {
  const lines = (key: string, aria: string, nameCell: ReactNode, labels: OcmLabel[] | null | undefined, id: ResourceIdentity | undefined, source = false, group = false) => {
    const all = labels ?? []
    const cls = (last: boolean) => `${rowClass('labels', source)} py-1 ${last ? 'border-b border-slate-100' : ''}`
    if (all.length === 0) {
      return (
        <div role="row" aria-label={aria} className={cls(true)}>
          {nameCell}
          <div role="cell" className="text-xs text-slate-400">{group ? '' : '—'}</div>
          <div role="cell" />
          <div role="cell" />
        </div>
      )
    }
    return all.map((label, i) => {
      const v = labelValue(label.value)
      return (
        <div key={`${key}/${i}`} role="row" aria-label={i === 0 ? aria : undefined} className={cls(i === all.length - 1)}>
          {i === 0 ? nameCell : <div role="cell" />}
          <div role="cell" className="truncate font-mono text-xs text-slate-500" title={label.name}>{label.name}</div>
          {v.kind === 'inline' ? (
            <div role="cell" className="truncate font-mono text-xs text-slate-700" title={v.text}>{v.text}</div>
          ) : (
            <div role="cell">
              <button type="button" aria-label={`Show ${label.name}`} onClick={() => p.onViewer({ kind: 'json', title: label.name, value: v.value })}
                className="inline-flex cursor-pointer items-center gap-1 font-mono text-xs text-slate-500 hover:text-blue-700">
                {'{…}'}
                <Maximize2 aria-hidden size={12} />
              </button>
            </div>
          )}
          <div role="cell" className="flex items-center justify-end gap-2">
            {label.name === VEX_LABEL && <VexActions id={id} subject={!source} label={label} p={p} />}
          </div>
        </div>
      )
    })
  }
  const row = (l: Line) => {
    if (l.kind === 'group') {
      const g = l.group
      return lines(l.key, lineLabel(l), <NameCell type={g.type} name={g.name} depth={0} count={g.children.length} expanded={p.expanded.has(l.key)} onToggle={() => p.onToggle(l.key)} />, [], undefined, false, true)
    }
    if (l.kind === 'platforms') return null // no platform children in this view
    const { kind, element: el } = l.item
    const id = kind === 'resource' ? { name: el.name, version: el.version, extraIdentity: el.extraIdentity ?? undefined } : undefined
    return lines(l.key, lineLabel(l), <NameCell type={el.type} source={kind === 'source'} name={l.label} depth={l.depth} note={l.note} />, el.labels, id, kind === 'source')
  }
  return (
    <div role="table" aria-label="Labels" className="text-sm">
      <div role="row" className={`${rowClass('labels')} border-b border-slate-300 py-1 text-xs font-semibold uppercase tracking-wide text-slate-500`}>
        <span role="columnheader">Name</span>
        <span role="columnheader">Label</span>
        <span role="columnheader">Value</span>
        <span role="columnheader" className="sr-only">Actions</span>
      </div>
      {p.componentLabels && lines('component', 'component itself', <NameCell type="component" name="component itself" depth={0} />, p.componentLabels, undefined)}
      <VirtualList items={p.lines} getKey={(l) => l.key} renderRow={row} role="rowgroup" itemRole="presentation" />
    </div>
  )
}
