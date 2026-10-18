import { useRef } from 'react'
import { useModal } from '../useModal'

// JsonModal: a value as pretty-printed JSON, centred like PullDialog.
export function JsonModal({ title, value, onClose }: { title: string; value: unknown; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  useModal(onClose, ref)
  return (
    <div className="fixed inset-0 z-10 flex items-start justify-center bg-black/30 pt-24" onClick={onClose}>
      <div ref={ref} tabIndex={-1} role="dialog" aria-modal="true" aria-label={title} className="w-[40rem] max-w-[95vw] rounded-lg bg-white p-4 shadow-xl" onClick={(e) => e.stopPropagation()}>
        <h2 className="mb-3 font-semibold">{title}</h2>
        <pre className="max-h-[60vh] overflow-auto rounded bg-slate-900 p-3 font-mono text-sm text-slate-100">{JSON.stringify(value, null, 2)}</pre>
        <div className="mt-3 flex justify-end">
          <button type="button" onClick={onClose} className="rounded-md border border-slate-300 px-2 py-1 text-sm hover:bg-slate-100">
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
