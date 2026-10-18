import { X } from 'lucide-react'
import { useRef, type ReactNode } from 'react'
import { useModal } from '../useModal'

// SidePanel: a panel fixed to the right (or left) edge; Escape or the close button closes it.
export function SidePanel({ title, onClose, side = 'right', children }: { title: string; onClose: () => void; side?: 'left' | 'right'; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  useModal(onClose, ref)
  return (
    <div ref={ref} tabIndex={-1} role="dialog" aria-label={title} className={`fixed inset-y-0 z-10 flex max-w-full flex-col border-slate-200 bg-white shadow-xl ${side === 'left' ? 'left-0 w-[28rem] border-r' : 'right-0 w-[48rem] border-l'}`}>
      <div className="flex items-center justify-between gap-2 border-b border-slate-200 px-4 py-3">
        <h2 className="truncate font-semibold">{title}</h2>
        <button type="button" aria-label="Close" onClick={onClose} className="rounded-md p-1 text-slate-500 hover:bg-slate-100">
          <X size={16} />
        </button>
      </div>
      <div className="flex-1 space-y-3 overflow-auto p-4">{children}</div>
    </div>
  )
}

export interface PanelLink { label: string; href: string; external?: boolean }

// PanelLinks: the download and raw links under a viewer.
export function PanelLinks({ links }: { links: PanelLink[] }) {
  return (
    <div className="flex flex-wrap gap-3 text-sm">
      {links.map((l) => (
        <a key={l.label} href={l.href} target="_blank" rel={l.external ? 'noreferrer noopener' : undefined} className="text-slate-700 underline hover:text-slate-900">
          {l.label}
        </a>
      ))}
    </div>
  )
}
