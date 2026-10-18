import type { LocatedDescriptor } from '../api/client'
import { descId, type OcmDescriptor } from '../api/ocm'

export interface Graph {
  readonly descs: ReadonlyMap<string, OcmDescriptor>
  readonly locations: ReadonlyMap<string, string>
  readonly rootLocation?: string
}

export interface TreeNode {
  id: string
  name: string
  version: string
  refName?: string
  loaded: boolean // descriptor present in the graph
  location?: string // registry the descriptor was read from
  foreign: boolean // read from another registry than the root
  cycle: boolean // already an ancestor: not expanded
  ancestors: readonly string[]
}

export interface TreeRow {
  key: string // index path, unique per position ('0', '0/1', …)
  node: TreeNode
  depth: number
  expandable: boolean
  expanded: boolean
}

// The first entry is the root: its location is the one others are compared to.
export function makeGraph(located: LocatedDescriptor[]): Graph {
  const id = (l: LocatedDescriptor) => descId(l.descriptor.component.name, l.descriptor.component.version)
  return {
    descs: new Map(located.map((l) => [id(l), l.descriptor])),
    locations: new Map(located.map((l) => [id(l), l.location])),
    rootLocation: located[0]?.location,
  }
}

function node(graph: Graph, name: string, version: string, refName: string | undefined, ancestors: readonly string[]): TreeNode {
  const id = descId(name, version)
  const location = graph.locations.get(id)
  return {
    id, name, version, refName, location,
    foreign: !!location && !!graph.rootLocation && location !== graph.rootLocation,
    loaded: graph.descs.has(id), cycle: ancestors.includes(id), ancestors,
  }
}

export function rootNode(graph: Graph, name: string, version: string): TreeNode {
  return node(graph, name, version, undefined, [])
}

// Children are computed on demand, so shared sub-trees are never built
// eagerly (a DAG with much sharing would explode as a tree).
export function childNodes(graph: Graph, n: TreeNode): TreeNode[] {
  if (n.cycle) return []
  const refs = graph.descs.get(n.id)?.component.componentReferences ?? []
  const ancestors = [...n.ancestors, n.id]
  return refs.map((r) => node(graph, r.componentName, r.version, r.name, ancestors))
}

export function flattenTree(graph: Graph, root: TreeNode, expanded: ReadonlySet<string>): TreeRow[] {
  const rows: TreeRow[] = []
  const visit = (n: TreeNode, depth: number, key: string) => {
    const children = childNodes(graph, n)
    const isExpanded = expanded.has(key) && children.length > 0
    rows.push({ key, node: n, depth, expandable: children.length > 0, expanded: isExpanded })
    if (isExpanded) children.forEach((c, i) => visit(c, depth + 1, `${key}/${i}`))
  }
  visit(root, 0, '0')
  return rows
}

// Rows of the tree filtered by a case-insensitive substring of name or
// version: matches plus all their ancestors (expanded); the root is always
// shown. Keys are the same index paths flattenTree uses. Stops at `limit`.
export function searchTree(graph: Graph, root: TreeNode, q: string, limit = 1000): TreeRow[] {
  const needle = q.trim().toLowerCase()
  const hits = (n: TreeNode) => n.name.toLowerCase().includes(needle) || n.version.toLowerCase().includes(needle)
  const rows: TreeRow[] = []
  // Ids whose whole subtree was walked without a match, a cycle cut-off or
  // the row limit: only then is "no match below" independent of the path.
  const barren = new Set<string>()
  // found: n or something below it matched (rows kept, else rolled back).
  // truncated: the walk below n was cut short by a cycle or the limit.
  const visit = (n: TreeNode, depth: number, key: string, force: boolean): { found: boolean; truncated: boolean } => {
    if (rows.length >= limit) return { found: false, truncated: true }
    if (barren.has(n.id)) return { found: false, truncated: false }
    const at = rows.length
    const children = childNodes(graph, n)
    const row: TreeRow = { key, node: n, depth, expandable: children.length > 0, expanded: false }
    rows.push(row)
    let found = hits(n)
    let truncated = n.cycle
    children.forEach((c, i) => {
      const r = visit(c, depth + 1, `${key}/${i}`, false)
      truncated ||= r.truncated
      if (r.found) {
        found = true
        row.expanded = true
      }
    })
    if (found || force) return { found: true, truncated }
    rows.length = at
    if (!truncated) barren.add(n.id)
    return { found: false, truncated }
  }
  visit(root, 0, '0', true)
  return rows
}
