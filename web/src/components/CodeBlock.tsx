import { CopyButton } from './CopyButton'

// CodeBlock shows copy-ready text (a command or config) in small monospace
// with an icon copy button inside the block, top right.
export function CodeBlock({ text, label }: { text: string; label: string }) {
  return (
    <div className="relative">
      <pre className="overflow-x-auto rounded bg-slate-100 py-1.5 pl-2 pr-8 font-mono text-xs">{text}</pre>
      <CopyButton text={text} label={label} className="absolute right-1 top-1" />
    </div>
  )
}
