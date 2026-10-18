import { useState } from 'react'
import { Container, Eye, GitBranch } from 'lucide-react'
import { stringify } from 'yaml'
import { resourceUrl } from '../../api/client'
import { identityString, sourceUrl } from '../../lib/contents'
import { isImage, resourceViewer } from '../../lib/contentsRows'
import type { ViewerScope, ViewerTarget } from '../viewers/ViewerPanel'
import { VirtualList } from '../VirtualList'
import { lineLabel, rowClass, type Line } from './grid'
import { ImageNameCell } from './ImageNameCell'
import { NameCell } from './NameCell'
import { platformName, PlatformRows, shortDigest } from './PlatformRows'

const iconButton = 'cursor-pointer text-slate-500 hover:text-blue-700'

// ContentsView: name | version | type | identity | actions. The name opens the element's YAML.
export function ContentsView({ lines, expanded, onToggle, scope, onPull, onViewer }: {
  lines: Line[]
  expanded: Set<string>
  onToggle: (key: string) => void
  scope: ViewerScope
  onPull: (reference: string) => void
  onViewer: (t: ViewerTarget) => void
}) {
  const { name, version, repository } = scope
  const [open, setOpen] = useState<string>()
  const row = (l: Line) => {
    if (l.kind === 'group') {
      const g = l.group
      return (
        <div role="row" aria-label={lineLabel(l)} data-depth="0" className={`${rowClass('contents')} border-b border-slate-100 py-1.5`}>
          <NameCell type={g.type} name={g.name} depth={0} count={g.children.length} expanded={expanded.has(l.key)} onToggle={() => onToggle(l.key)} />
          <div role="cell" className="truncate text-slate-600">{g.version}</div>
          <div role="cell" className="truncate text-slate-600">{g.type}</div>
          <div role="cell" />
          <div role="cell" />
        </div>
      )
    }
    if (l.kind === 'platforms') {
      const el = l.item.element
      return (
        <PlatformRows
          scope={scope}
          line={l}
          view="contents"
          render={(index, p) => {
            const title = p.platform ? `${el.name} ${p.platform}` : el.name
            const ref = `${index.repository}@${p.digest}`
            return (
              <div key={p.digest} role="row" aria-label={`platform ${platformName(p.platform)}`} data-depth={l.depth} className={`${rowClass('contents')} border-b border-slate-100 py-1.5`}>
                <NameCell name={platformName(p.platform)} depth={l.depth} />
                <div role="cell" className="truncate font-mono text-xs text-slate-600" title={p.digest}>{shortDigest(p.digest)}</div>
                <div role="cell" />
                <div role="cell" />
                <div role="cell" className="flex items-center justify-end gap-2">
                  <button type="button" aria-label={`View ${title}`} onClick={() => onViewer({ kind: 'image', title, resource: el.name, extraIdentity: el.extraIdentity, digest: p.digest })} className={iconButton}>
                    <Eye aria-hidden size={16} />
                  </button>
                  <button type="button" aria-label={`docker pull ${title}`} onClick={() => onPull(ref)} className={iconButton}>
                    <Container aria-hidden size={16} />
                  </button>
                </div>
              </div>
            )
          }}
        />
      )
    }
    const { kind, element: el } = l.item
    const source = kind === 'source'
    const link = source ? sourceUrl(el) : undefined
    const imageRef = !source && isImage(el.type) && typeof el.access?.imageReference === 'string' ? el.access.imageReference : undefined
    const identity = identityString(el.extraIdentity)
    return (
      <>
        <div role="row" aria-label={lineLabel(l)} data-depth={l.depth} className={`${rowClass('contents', source)} border-b border-slate-100 py-1.5`}>
          {!source && isImage(el.type) ? (
            <ImageNameCell scope={scope} item={l.item} depth={l.depth} label={l.label} note={l.note} expanded={expanded.has(l.key)} onToggle={() => onToggle(l.key)} onOpen={() => setOpen(open === l.key ? undefined : l.key)} open={open === l.key} />
          ) : (
            <NameCell type={el.type} source={source} name={l.label} depth={l.depth} note={l.note} onOpen={() => setOpen(open === l.key ? undefined : l.key)} open={open === l.key} />
          )}
          <div role="cell" className="truncate text-slate-600" title={el.version}>{el.version}</div>
          <div role="cell" className="truncate text-slate-600" title={el.type}>{el.type}</div>
          <div role="cell" className="truncate text-slate-500" title={identity || undefined}>{identity}</div>
          <div role="cell" className="flex items-center justify-end gap-2">
            {resourceViewer(el) && !source && (
              <button type="button" aria-label={`View ${el.name}`} onClick={() => onViewer({ kind: 'document', part: 'sbom', title: el.name, source: { resource: el.name, extraIdentity: el.extraIdentity } })} className={iconButton}>
                <Eye aria-hidden size={16} />
              </button>
            )}
            {imageRef && (
              <button type="button" aria-label={`docker pull ${el.name}`} onClick={() => onPull(imageRef)} className={iconButton}>
                <Container aria-hidden size={16} />
              </button>
            )}
            {!source && !imageRef && (
              <a href={resourceUrl(name, version, el.name, el.extraIdentity, repository)} download aria-label={`Download ${el.name}`} className={iconButton}>
                ⬇
              </a>
            )}
            {link && (
              <a href={link} target="_blank" rel="noreferrer" aria-label={`Open source ${el.name}`} className={iconButton}>
                <GitBranch aria-hidden size={16} />
              </a>
            )}
          </div>
        </div>
        {open === l.key && (
          <div role="row" className="grid">
            <div role="cell">
              <pre className="overflow-auto bg-slate-900 p-3 text-xs text-slate-100">{stringify(el)}</pre>
            </div>
          </div>
        )}
      </>
    )
  }
  return (
    <div role="table" aria-label="Contents" className="text-sm">
      <div role="row" className={`${rowClass('contents')} border-b border-slate-300 py-1 text-xs font-semibold uppercase tracking-wide text-slate-500`}>
        <span role="columnheader">Name</span>
        <span role="columnheader">Version</span>
        <span role="columnheader">Type</span>
        <span role="columnheader">Identity</span>
        <span role="columnheader" className="sr-only">Actions</span>
      </div>
      <VirtualList items={lines} getKey={(l) => l.key} renderRow={row} role="rowgroup" itemRole="presentation" />
    </div>
  )
}
