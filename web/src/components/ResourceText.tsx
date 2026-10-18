import { useQuery } from '@tanstack/react-query'
import ReactMarkdown from 'react-markdown'
import { ApiError } from '../api/client'
import { TEXT_LIMIT, textKind, type TextKind } from '../lib/resourceText'
import { ErrorPanel } from './ErrorPanel'

async function fetchResourceText(url: string, name: string, what: string): Promise<{ kind: TextKind; text?: string }> {
  const res = await globalThis.fetch(url)
  if (!res.ok) throw new ApiError(res.status, 'RESOURCE_FETCH_FAILED', `${what} request failed with status ${res.status}`)
  const len = res.headers.get('Content-Length')
  const kind = textKind(res.headers.get('Content-Type'), name, len === null ? null : Number(len))
  if (kind === 'download') {
    await res.body?.cancel()
    return { kind }
  }
  const text = await res.text()
  return text.length > TEXT_LIMIT ? { kind: 'download' } : { kind, text }
}

// Only absolute http(s) URLs: relative ones cannot be resolved against an
// OCM resource, other schemes are unsafe. null drops the attribute.
export function absoluteOnly(url: string): string | null {
  return /^https?:\/\//i.test(url) ? url : null
}

// A text resource rendered as markdown, plain text or a download link.
// safeLinks keeps only absolute http(s) link and image URLs.
export function ResourceText({ url, name, what, safeLinks = false }: { url: string; name: string; what: string; safeLinks?: boolean }) {
  const q = useQuery({ queryKey: ['resourceText', url], queryFn: () => fetchResourceText(url, name, what) })
  if (q.isError) return <ErrorPanel error={q.error} />
  if (!q.data) return <p className="text-sm text-slate-500">Loading…</p>
  if (q.data.kind === 'download') {
    return <a href={url} download className="text-sm text-blue-700 underline">Download {what} ({name})</a>
  }
  if (q.data.kind === 'text') return <pre className="whitespace-pre-wrap text-xs">{q.data.text}</pre>
  return (
    <div className="space-y-2 text-sm [&_h1]:text-lg [&_h1]:font-semibold [&_h2]:font-semibold [&_ul]:list-disc [&_ul]:pl-5 [&_a]:text-blue-700 [&_a]:underline">
      <ReactMarkdown
        urlTransform={safeLinks ? absoluteOnly : undefined}
        components={safeLinks ? { img: ({ src, alt }) => (src ? <img src={String(src)} alt={alt ?? ''} /> : null) } : undefined}
      >
        {q.data.text ?? ''}
      </ReactMarkdown>
    </div>
  )
}
