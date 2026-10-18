import { useState } from 'react'
import { displayLocation, type LocatedDescriptor } from '../api/client'
import { flattenTree, makeGraph, rootNode, searchTree, type TreeRow } from '../lib/tree'
import { ComponentIcon } from './TypeIcon'
import { VirtualList } from './VirtualList'

export function ComponentTree({ located, rootName, rootVersion, selectedId, query, onSelect }: {
  located: LocatedDescriptor[]
  rootName: string
  rootVersion: string
  selectedId: string
  query?: string // when non-empty, only matches and their ancestors, all open
  onSelect: (name: string, version: string, location?: string) => void
}) {
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set(['0']))
  const graph = makeGraph(located)
  const searching = !!query?.trim()
  const root = rootNode(graph, rootName, rootVersion)
  const rows = searching ? searchTree(graph, root, query ?? '') : flattenTree(graph, root, expanded)
  const toggle = (key: string) =>
    setExpanded((s) => {
      const next = new Set(s)
      if (!next.delete(key)) next.add(key)
      return next
    })
  const row = ({ key, node, depth, expandable, expanded: isOpen }: TreeRow) => (
    <div className="flex items-center gap-1 text-sm" style={{ paddingLeft: depth * 14 }}>
      {expandable && searching ? (
        <span aria-hidden className="w-4 text-slate-500">
          {isOpen ? '▾' : '▸'}
        </span>
      ) : expandable ? (
        <button type="button" aria-label={`${isOpen ? 'Collapse' : 'Expand'} ${node.name}`} onClick={() => toggle(key)} className="w-4 text-slate-500">
          {isOpen ? '▾' : '▸'}
        </button>
      ) : (
        <span className="w-4" />
      )}
      <button
        type="button"
        title={node.name}
        aria-current={node.id === selectedId}
        onClick={() => onSelect(node.name, node.version, node.foreign ? node.location : undefined)}
        className={`flex min-w-0 items-center gap-1 truncate rounded px-1 text-left ${node.id === selectedId ? 'bg-blue-100 font-semibold' : 'hover:bg-slate-100'}`}
      >
        <ComponentIcon /> {node.name.split('/').pop()} <span className="text-slate-500">{node.version}</span>
        {node.foreign && node.location && (
          <span
            className="ml-1 rounded bg-amber-100 px-1 text-xs text-amber-800"
            title={`Not found in the selected registry; resolved via the configured resolvers from ${displayLocation(node.location)}`}
          >
            from {displayLocation(node.location)}
          </span>
        )}
        {node.cycle && <span title="cycle"> ↻</span>}
      </button>
    </div>
  )
  return (
    <div className="space-y-2">
      <VirtualList
        items={rows}
        getKey={(r) => r.key}
        renderRow={row}
        label="Components"
        role="tree"
        itemRole="treeitem"
        itemLabel={(r) => `${r.node.name} ${r.node.version}`}
      />
    </div>
  )
}
