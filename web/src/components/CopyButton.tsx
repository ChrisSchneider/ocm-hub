import { Check, Copy, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

// Clipboard API where available (secure contexts only), else a hidden
// textarea and execCommand('copy') — e.g. over plain http on a LAN host.
async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // fall through to the legacy path
  }
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.opacity = '0'
  document.body.appendChild(area)
  try {
    area.select()
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    area.remove()
  }
}

// CopyButton is an icon button; label is its accessible name (and switches to
// "Copied" / "Copy failed" for a moment after a click). The title shows the text.
export function CopyButton({ text, label, tone = 'light', className = '' }: { text: string; label: string; tone?: 'light' | 'dark'; className?: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])
  const copy = async () => {
    const ok = await copyText(text)
    setState(ok ? 'copied' : 'failed')
    clearTimeout(timer.current)
    timer.current = setTimeout(() => setState('idle'), 1500)
  }
  const Icon = state === 'copied' ? Check : state === 'failed' ? X : Copy
  const colors = {
    light: { idle: 'text-slate-500 hover:bg-slate-200 hover:text-slate-900', copied: 'text-slate-900', failed: 'text-slate-900' },
    dark: { idle: 'text-slate-300 hover:bg-slate-700 hover:text-white', copied: 'text-white', failed: 'text-white' },
  }[tone][state]
  return (
    <button
      type="button"
      onClick={copy}
      title={text}
      aria-label={state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : label}
      className={`rounded p-1 ${colors} ${className}`}
    >
      <Icon size={14} aria-hidden />
    </button>
  )
}
