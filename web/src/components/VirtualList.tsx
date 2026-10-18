import { useVirtualizer } from '@tanstack/react-virtual'
import { useRef, type ReactNode } from 'react'

// Renders every row up to threshold; above it only the visible rows.
export function VirtualList<T>({ items, getKey, renderRow, threshold = 100, maxHeight = 640, label, role = 'list', itemRole = 'listitem', itemLabel }: {
  items: T[]
  getKey: (item: T) => string
  renderRow: (item: T) => ReactNode
  threshold?: number
  maxHeight?: number
  label?: string
  role?: string
  itemRole?: string
  itemLabel?: (item: T) => string
}) {
  const parentRef = useRef<HTMLDivElement>(null)
  const virtual = items.length > threshold
  const v = useVirtualizer({ count: virtual ? items.length : 0, getScrollElement: () => parentRef.current, estimateSize: () => 36, overscan: 10 })
  if (!virtual) {
    return (
      <div role={role} aria-label={label}>
        {items.map((it) => (
          <div role={itemRole} aria-label={itemLabel?.(it)} key={getKey(it)}>
            {renderRow(it)}
          </div>
        ))}
      </div>
    )
  }
  return (
    <div ref={parentRef} style={{ maxHeight, overflow: 'auto' }}>
      <div role={role} aria-label={label} style={{ height: v.getTotalSize(), position: 'relative' }}>
        {v.getVirtualItems().map((vi) => {
          const it = items[vi.index]
          return (
            <div
              role={itemRole}
              aria-label={itemLabel?.(it)}
              key={getKey(it)}
              data-index={vi.index}
              ref={v.measureElement}
              style={{ position: 'absolute', top: 0, left: 0, right: 0, transform: `translateY(${vi.start}px)` }}
            >
              {renderRow(it)}
            </div>
          )
        })}
      </div>
    </div>
  )
}
