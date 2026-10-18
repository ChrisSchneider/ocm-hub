import { useRef } from 'react'
import { CopyButton } from './CopyButton'
import { useModal } from './useModal'

export function PullDialog({ reference, onClose }: { reference: string; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  useModal(onClose, ref)
  const command = `docker pull ${reference}`
  return (
    <div className="fixed inset-0 z-10 flex items-start justify-center bg-black/30 pt-24" onClick={onClose}>
      <div ref={ref} tabIndex={-1} role="dialog" aria-modal="true" aria-label="Pull image" className="w-[32rem] max-w-[95vw] rounded-lg bg-white p-4 shadow-xl" onClick={(e) => e.stopPropagation()}>
        <h2 className="mb-3 font-semibold">Pull image</h2>
        <code className="block break-all rounded bg-slate-900 p-3 font-mono text-sm text-slate-100">{command}</code>
        <div className="mt-3 flex justify-end gap-2">
          <CopyButton text={command} label="copy" />
          <button type="button" onClick={onClose} className="rounded-md border border-slate-300 px-2 py-1 text-sm hover:bg-slate-100">
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
