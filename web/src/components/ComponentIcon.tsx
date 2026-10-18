import { DynamicIcon, type IconName } from 'lucide-react/dynamic'
import { useState } from 'react'

function Placeholder({ title, size }: { title: string; size: number }) {
  const letter = (title.split('/').pop() ?? title).charAt(0).toUpperCase()
  return (
    <div aria-hidden className="flex items-center justify-center rounded-md bg-slate-200 font-semibold text-slate-600" style={{ width: size, height: size }}>
      {letter}
    </div>
  )
}

// icon: https/data URL, or lucide/<name>; anything else (or a load error)
// shows a letter placeholder.
export function ComponentIcon(props: { icon?: string; title: string; size?: number }) {
  // Keyed by icon so a load failure does not outlive the icon it belongs to.
  return <ComponentIconInner key={props.icon} {...props} />
}

function ComponentIconInner({ icon, title, size = 40 }: { icon?: string; title: string; size?: number }) {
  const [failed, setFailed] = useState(false)
  if (!icon || failed) return <Placeholder title={title} size={size} />
  if (icon.startsWith('lucide/')) {
    return <DynamicIcon name={icon.slice('lucide/'.length) as IconName} size={size} aria-hidden fallback={() => <Placeholder title={title} size={size} />} />
  }
  if (icon.startsWith('https://') || icon.startsWith('data:')) {
    return <img src={icon} alt="" width={size} height={size} referrerPolicy="no-referrer" onError={() => setFailed(true)} className="rounded-md" />
  }
  return <Placeholder title={title} size={size} />
}
