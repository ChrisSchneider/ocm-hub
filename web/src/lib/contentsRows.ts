import type { OcmElement } from '../api/ocm'
import { identityString, type ContentItem } from './contents'

export type View = 'contents' | 'labels' | 'supply-chain'

export interface GroupRow {
  kind: 'group'
  key: string
  name: string
  type?: string
  version: string
  children: ContentItem[]
}
export type TopRow = ContentItem | GroupRow

// groupItems folds resources sharing name, type and version (two or more) into
// one row at the first member's position; everything else keeps its order.
export function groupItems(items: ContentItem[]): TopRow[] {
  const gkey = (i: ContentItem) => `group/${i.element.name}/${i.element.type ?? ''}/${i.element.version}`
  const members = new Map<string, ContentItem[]>()
  for (const i of items) if (i.kind === 'resource') members.set(gkey(i), [...(members.get(gkey(i)) ?? []), i])
  const rows: TopRow[] = []
  for (const i of items) {
    const m = i.kind === 'resource' ? members.get(gkey(i)) : undefined
    if (!m || m.length < 2) rows.push(i)
    else if (m[0] === i) rows.push({ kind: 'group', key: gkey(i), name: i.element.name, type: i.element.type, version: i.element.version, children: m })
  }
  return rows
}

export function childName(extra?: Record<string, string> | null): string {
  return extra?.os && extra?.architecture ? `${extra.os}/${extra.architecture}` : identityString(extra)
}

export const isImage = (type?: string): boolean => type === 'ociImage' || type === 'ociArtifact'

export function resourceViewer(el: OcmElement): 'sbom' | undefined {
  const mt = el.access?.mediaType
  return el.type === 'sbom' && (mt === undefined || (typeof mt === 'string' && mt.includes('json'))) ? 'sbom' : undefined
}
