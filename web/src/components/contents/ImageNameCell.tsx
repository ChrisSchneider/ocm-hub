import type { ContentItem } from '../../lib/contents'
import { NameCell } from './NameCell'
import { useImage } from './useImage'
import type { ViewerScope } from '../viewers/ViewerPanel'

// ImageNameCell: the name cell of an image row with its platforms toggle; the count shows once known.
export function ImageNameCell({ scope, item, depth, label, note, expanded, onToggle, onOpen, open }: {
  scope: ViewerScope
  item: ContentItem
  depth: 0 | 1 | 2
  label: string
  note?: string
  expanded: boolean
  onToggle: () => void
  onOpen?: () => void
  open?: boolean
}) {
  const el = item.element
  const q = useImage(scope, el.name, el.extraIdentity, expanded)
  return <NameCell type={el.type} name={label} depth={depth} note={note} count={q.data?.platforms.length} expanded={expanded} onToggle={onToggle} onOpen={onOpen} open={open} />
}
