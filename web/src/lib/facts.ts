import type { VersionOverview } from '../api/client'

type Signature = VersionOverview['signatures'][number]
type Source = NonNullable<VersionOverview['sources']>[number]
type Resource = VersionOverview['resources'][number]

const isSigstore = (s: Signature) => /sigstore/i.test(s.signature.algorithm) || /sigstore/i.test(s.signature.mediaType)
const isCertificate = (s: Signature) => s.signature.mediaType === 'application/x-pem-file' || !!s.signature.issuer

// signatureLabel: the signature algorithm(s), each with "certificate" or
// "sigstore" when that can be told from the signature ("RSASSA-PSS · certificate").
export function signatureLabel(sigs: Signature[]): string {
  const labels = sigs.map((s) => {
    const algo = s.signature.algorithm
    if (isSigstore(s)) return algo.toLowerCase() === 'sigstore' ? 'sigstore' : `${algo} · sigstore`
    return isCertificate(s) ? `${algo} · certificate` : algo
  })
  return [...new Set(labels)].join(', ')
}

// sourceLink: the first source with a repository URL ("github.com/acme/app @ 0123456").
export function sourceLink(sources: Source[]): { label: string; href: string } | undefined {
  for (const s of sources) {
    const a = (s.access ?? {}) as Record<string, unknown>
    const repo = typeof a.repoUrl === 'string' ? a.repoUrl : typeof a.repository === 'string' ? a.repository : undefined
    if (!repo || !/^https?:\/\//.test(repo)) continue
    const base = repo.replace(/\.git$/, '').replace(/\/$/, '')
    const commit = typeof a.commit === 'string' ? a.commit : undefined
    const name = base.replace(/^https?:\/\//, '')
    return commit ? { label: `${name} @ ${commit.slice(0, 7)}`, href: `${base}/tree/${commit}` } : { label: name, href: base }
  }
  return undefined
}

// totalSize: the sum of the resources' sizes, only when every resource carries one.
export function totalSize(resources: Resource[]): number | undefined {
  if (resources.length === 0) return undefined
  let sum = 0
  for (const r of resources) {
    const size = (r.access as Record<string, unknown> | undefined)?.size
    if (typeof size !== 'number') return undefined
    sum += size
  }
  return sum
}

// plural: "1 package", "3 packages".
export const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

// formatBytes in decimal units ("1.5 kB", "12 MB").
export function formatBytes(n: number): string {
  const units = ['B', 'kB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return `${i === 0 ? v : Number(v.toFixed(v < 10 ? 1 : 0))} ${units[i]}`
}
