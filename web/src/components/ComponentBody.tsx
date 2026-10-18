import { ListTree } from 'lucide-react'
import { useNavigate } from '@tanstack/react-router'
import { descId, parseId } from '../api/ocm'
import type { View } from '../lib/contentsRows'
import type { ComponentSearch } from '../router'
import { ComponentTree } from './ComponentTree'
import { ErrorPanel } from './ErrorPanel'
import { SelectedPanel } from './SelectedPanel'
import { useComponentTree } from './useComponentTree'

// Master–detail: the selected component left (its full descriptor, fetched on
// selection), the reference tree right (one compact recursive call).
export function ComponentBody({ name, version, search }: { name: string; version: string; search: ComponentSearch }) {
  const navigate = useNavigate({ from: '/components/$' })
  const repository = search.repository
  const { tree, root, located } = useComponentTree(name, version, repository)
  const selected = parseId(search.select ?? descId(name, version))
  const isRoot = selected.name === name && selected.version === version
  // A reference resolved from another registry is read from there.
  const selectedRepository = isRoot ? repository : search.selectRepository
  const view: View = search.view === 'labels' || search.view === 'attestations' ? search.view : 'contents'
  return (
    <div className="flex gap-6">
      <SelectedPanel
        name={selected.name}
        version={selected.version}
        repository={selectedRepository}
        query={search.q}
        openable={!isRoot}
        view={view}
        onView={(v) => void navigate({ search: (s) => ({ ...s, view: v }) })}
      />
      <aside className="w-72 shrink-0" aria-labelledby="component-tree-heading">
        <h2 id="component-tree-heading" className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-slate-500">
          <ListTree size={14} aria-hidden />
          Component tree
          {located && ' '}
          {located && <span className="font-normal normal-case text-slate-400">({located.length})</span>}
        </h2>
        {tree.isError && <ErrorPanel error={tree.error} />}
        {located ? (
          <ComponentTree
            located={located}
            rootName={name}
            rootVersion={version}
            query={search.q}
            selectedId={descId(selected.name, selected.version)}
            onSelect={(n, v, loc) => void navigate({ search: (s) => ({ ...s, select: n === name && v === version ? undefined : descId(n, v), selectRepository: n === name && v === version ? undefined : loc }) })}
          />
        ) : tree.isError ? (
          root.isError ? <ErrorPanel error={root.error} /> : <p className="text-sm text-slate-500">Loading…</p>
        ) : (
          <p className="text-sm text-slate-500">Loading…</p>
        )}
      </aside>
    </div>
  )
}
