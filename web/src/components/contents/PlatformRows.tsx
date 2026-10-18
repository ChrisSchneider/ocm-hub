import type { ReactNode } from 'react'
import type { ImageIndex } from '../../api/client'
import type { View } from '../../lib/contentsRows'
import type { ViewerScope } from '../viewers/ViewerPanel'
import { INDENT } from './NameCell'
import { rowClass, type Line } from './grid'
import { useImage } from './useImage'

// PlatformRows: the child rows of an expanded image row. While the platforms load, or when they
// fail, one row says so; otherwise each platform renders through render.
export function PlatformRows({ scope, line, view, render }: {
  scope: ViewerScope
  line: Extract<Line, { kind: 'platforms' }>
  view: View
  render: (index: ImageIndex, platform: ImageIndex['platforms'][number]) => ReactNode
}) {
  const el = line.item.element
  const q = useImage(scope, el.name, el.extraIdentity, true)
  if (q.data) return <>{q.data.platforms.map((p) => render(q.data, p))}</>
  return (
    <div role="row" data-depth={line.depth} className={`${rowClass(view)} border-b border-slate-100 py-1`}>
      <div role="cell" className={`text-xs ${INDENT[line.depth]} ${q.isError ? 'text-slate-600' : 'text-slate-500'}`}>
        {q.isError ? q.error.message : 'Loading platforms…'}
      </div>
    </div>
  )
}

// platformName: the name of a platform row; "" (a single manifest without os/arch) is —.
export const platformName = (platform: string) => platform || '—'

// shortDigest: sha256: and the first 12 hex digits.
export const shortDigest = (digest: string) => digest.slice(0, 'sha256:'.length + 12)
