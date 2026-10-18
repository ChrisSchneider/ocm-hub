import type { OcmDescriptor, OcmElement } from '../api/ocm'

export interface ContentItem {
  kind: 'source' | 'resource'
  element: OcmElement
  key: string
}

export function identityString(extra?: Record<string, string> | null): string {
  return Object.entries(extra ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, v]) => `${k}=${v}`)
    .join(', ')
}

// Sources first (usually one), then resources, in descriptor order.
export function contentItems(d: OcmDescriptor): ContentItem[] {
  const items: ContentItem[] = []
  const add = (kind: ContentItem['kind'], els: OcmElement[] | null | undefined) =>
    (els ?? []).forEach((element, i) => items.push({ kind, element, key: `${kind}/${i}` }))
  add('source', d.component.sources)
  add('resource', d.component.resources)
  return items
}

// Counts resources only; sources are filtered through their own chip.
export function typeCounts(items: ContentItem[]): [string, number][] {
  const counts = new Map<string, number>()
  for (const { kind, element } of items) {
    if (kind !== 'resource') continue
    const t = element.type ?? 'unknown'
    counts.set(t, (counts.get(t) ?? 0) + 1)
  }
  return [...counts].sort(([a, x], [b, y]) => y - x || a.localeCompare(b))
}

function repoBase(raw: string): URL | undefined {
  let s = raw.trim()
  const ssh = /^git@([^:/]+):(.+)$/.exec(s)
  if (ssh) s = `https://${ssh[1]}/${ssh[2]}`
  else if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(s)) s = `https://${s}`
  try {
    const u = new URL(s)
    if (u.protocol !== 'https:' && u.protocol !== 'http:') return undefined
    u.pathname = u.pathname.replace(/\/+$/, '').replace(/\.git$/, '').replace(/\/+$/, '')
    u.search = ''
    u.hash = ''
    return u
  } catch {
    return undefined
  }
}

// Browsable URL of a source, pinned to its commit on GitHub/GitLab hosts.
export function sourceUrl(el: OcmElement): string | undefined {
  const access = el.access
  const raw = ['repoUrl', 'repository', 'url'].map((k) => access?.[k]).find((v): v is string => typeof v === 'string')
  if (!raw) return undefined
  const u = repoBase(raw)
  if (!u) return undefined
  const base = `${u.protocol}//${u.host}${u.pathname}`
  const commit = access?.commit
  if (typeof commit !== 'string' || !commit) return base
  if (u.hostname === 'github.com') return `${base}/tree/${encodeURIComponent(commit)}`
  if (u.hostname.includes('gitlab')) return `${base}/-/tree/${encodeURIComponent(commit)}`
  return base
}
