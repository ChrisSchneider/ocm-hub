export const TEXT_LIMIT = 1 << 20

export type TextKind = 'markdown' | 'text' | 'download'

// How a text resource (changelog, documentation) is shown. Markdown:
// text/markdown, a *.md name, or no media type (served as
// application/octet-stream); text/plain as <pre>; everything else and
// anything over TEXT_LIMIT as a download.
export function textKind(contentType: string | null, resourceName: string, size: number | null): TextKind {
  if (size !== null && size > TEXT_LIMIT) return 'download'
  const type = (contentType ?? '').split(';')[0].trim().toLowerCase()
  if (type === 'text/markdown' || resourceName.toLowerCase().endsWith('.md') || type === '' || type === 'application/octet-stream') return 'markdown'
  if (type === 'text/plain') return 'text'
  return 'download'
}
