import type { ContentItem } from '../../lib/contents'
import type { GroupRow, View } from '../../lib/contentsRows'

// GRID: the column template of each view; every row of a view's table uses it,
// so columns line up across rows.
export const GRID: Record<View, string> = {
  contents: 'grid-cols-[minmax(0,1.4fr)_7rem_9rem_minmax(0,1fr)_5rem]',
  labels: 'grid-cols-[minmax(0,1fr)_14rem_20rem_4rem]',
  'supply-chain': 'grid-cols-[minmax(0,1fr)_10rem_8rem_minmax(0,11rem)_minmax(0,1fr)_3rem_2rem]',
}

// Line: one top-level entry of the table (a group parent or an item) as the
// views list it (platforms: the children of an expanded image row); an item's label is its shown name (group children by
// identity), note what it is nested under.
export type Line =
  | { kind: 'group'; key: string; group: GroupRow }
  | { kind: 'platforms'; key: string; item: ContentItem; depth: 1 | 2 }
  | { kind: 'item'; key: string; item: ContentItem; depth: 0 | 1 | 2; label: string; note?: string }

export const lineLabel = (l: Line): string => (l.kind === 'group' ? `group ${l.group.name}` : l.kind === 'platforms' ? `platforms ${l.item.element.name}` : `${l.item.kind} ${l.label}`)

export const rowClass = (view: View, source = false) => `grid ${GRID[view]} items-center gap-x-3 px-2 ${source ? 'bg-slate-50' : ''}`
