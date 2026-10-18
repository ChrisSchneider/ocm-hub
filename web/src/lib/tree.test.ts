import type { OcmDescriptor } from '../api/ocm'
import { desc, howTo, located } from '../test/fixtures'
import { childNodes, flattenTree, makeGraph, rootNode, searchTree } from './tree'

const graphOf = (d: OcmDescriptor[]) => makeGraph(located(d))

const label = (rows: ReturnType<typeof flattenTree>) => rows.map((r) => `${'  '.repeat(r.depth)}${r.node.name.split('/').pop()}`)

test('shows a shared child under each parent, in reference order', () => {
  const g = graphOf(howTo)
  const root = rootNode(g, 'ocm.software/how-to/product', '2.0.0')
  const all = new Set(['0', '0/0', '0/1', '0/2'])
  expect(label(flattenTree(g, root, all))).toEqual(['product', '  backend', '    cache', '  frontend', '  cache'])
})

test('only expanded rows are listed; root expanded shows the first level', () => {
  const g = graphOf(howTo)
  const rows = flattenTree(g, rootNode(g, 'ocm.software/how-to/product', '2.0.0'), new Set(['0']))
  expect(rows.map((r) => r.key)).toEqual(['0', '0/0', '0/1', '0/2'])
  expect(rows[1]).toMatchObject({ expandable: true, expanded: false, depth: 1 })
  expect(rows[2]).toMatchObject({ expandable: false })
})

test('children missing from the list are leaves, not loaded', () => {
  const g = graphOf([howTo[0]])
  const kids = childNodes(g, rootNode(g, 'ocm.software/how-to/product', '2.0.0'))
  expect(kids.map((k) => [k.refName, k.loaded])).toEqual([['backend', false], ['frontend', false], ['cache', false]])
  expect(childNodes(g, kids[0])).toEqual([])
})

test('a cycle is marked and not expanded further', () => {
  const g = graphOf([desc('a.org/a', '1', [['b', 'a.org/b', '1']]), desc('a.org/b', '1', [['a', 'a.org/a', '1']])])
  const a = rootNode(g, 'a.org/a', '1')
  const [b] = childNodes(g, a)
  const [a2] = childNodes(g, b)
  expect(a2).toMatchObject({ id: 'a.org/a:1', cycle: true })
  expect(childNodes(g, a2)).toEqual([])
  expect(flattenTree(g, a, new Set(['0', '0/0', '0/0/0'])).at(-1)).toMatchObject({ expandable: false })
})

const search = (g: ReturnType<typeof makeGraph>, q: string, limit?: number) =>
  searchTree(g, rootNode(g, 'ocm.software/how-to/product', '2.0.0'), q, limit)

test('search keeps matches and their ancestors, expanded; a shared match shows under both parents', () => {
  const rows = search(graphOf(howTo), 'cache')
  expect(label(rows)).toEqual(['product', '  backend', '    cache', '  cache'])
  expect(rows.map((r) => r.key)).toEqual(['0', '0/0', '0/0/0', '0/2'])
  expect(rows[0]).toMatchObject({ expanded: true, expandable: true })
  expect(rows[1]).toMatchObject({ expanded: true })
  expect(rows[2]).toMatchObject({ expanded: false, expandable: false })
})

test('search hides non-matching branches, matches case-insensitively on name or version', () => {
  const g = graphOf(howTo)
  expect(label(search(g, 'FRONT'))).toEqual(['product', '  frontend'])
  expect(label(search(g, '3.1.0'))).toEqual(['product', '  backend'])
  expect(label(search(g, 'nothing'))).toEqual(['product'])
})

test('search is cycle safe', () => {
  const g = graphOf([desc('a/x', '1', [['y', 'a/y', '1']]), desc('a/y', '1', [['x', 'a/x', '1']])])
  const rows = searchTree(g, rootNode(g, 'a/x', '1'), 'x')
  expect(label(rows)).toEqual(['x', '  y', '    x'])
  expect(rows[2].node.cycle).toBe(true)
})

test('search stops at the row limit', () => {
  expect(search(graphOf(howTo), 'o', 2)).toHaveLength(2)
})

test('search: a cycle cut-off on one path does not hide matches reached on another', () => {
  const g = graphOf([
    desc('x/r', '1', [['a', 'x/a', '1'], ['b', 'x/b', '1']]),
    desc('x/b', '1', [['a', 'x/a', '1']]),
    desc('x/a', '1', [['d', 'x/d', '1'], ['c', 'x/c', '1']]),
    desc('x/d', '1', [['a', 'x/a', '1']]),
    desc('x/c', '1'),
  ])
  const rows = searchTree(g, rootNode(g, 'x/r', '1'), 'c')
  expect(rows.map((r) => r.key)).toEqual(['0', '0/0', '0/0/1', '0/1', '0/1/0', '0/1/0/1'])
  expect(label(rows)).toEqual(['r', '  a', '    c', '  b', '    a', '      c'])
})

test('search: a subtree truncated by the limit is not remembered as barren', () => {
  // limit 3 cuts the walk below r > a > m; m must still be walked via r > m.
  const g = graphOf([
    desc('x/r', '1', [['a', 'x/a', '1'], ['m', 'x/m', '1']]),
    desc('x/a', '1', [['m', 'x/m', '1']]),
    desc('x/m', '1', [['h', 'x/h', '1']]),
    desc('x/h', '1'),
  ])
  expect(label(searchTree(g, rootNode(g, 'x/r', '1'), 'h', 3))).toEqual(['r', '  m', '    h'])
})

test('marks references read from another location', () => {
  const l = [
    { location: 'oci::ghcr.io/a', descriptor: desc('acme.org/root', '1.0.0', [['dep', 'acme.org/dep', '1.0.0']]) },
    { location: 'oci::ghcr.io/b', descriptor: desc('acme.org/dep', '1.0.0') },
  ]
  const g = makeGraph(l)
  const root = rootNode(g, 'acme.org/root', '1.0.0')
  const [child] = childNodes(g, root)
  expect(root.foreign).toBe(false)
  expect(child.location).toBe('oci::ghcr.io/b')
  expect(child.foreign).toBe(true)
})
