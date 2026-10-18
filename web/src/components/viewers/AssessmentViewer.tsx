import { useState } from 'react'
import type { components } from '../../api/schema'
import { countLine, FilterBox, matches } from './DocTable'

type Assessment = components['schemas']['DocumentAssessment']
type Finding = Assessment['findings'][number]

// statusLabel: the OSCAL status in words; partial is "teilweise" (some targets
// pass), reason other on a heuristic means it found nothing against it.
export function statusLabel(f: Finding): string {
  if (f.state === 'satisfied') return 'Satisfied'
  if (f.implementation === 'partial') return 'Partially satisfied'
  if (f.reason === 'other') return 'Not disproven'
  return f.state === 'not-satisfied' ? 'Not satisfied' : f.state
}

function FindingCard({ f }: { f: Finding }) {
  return (
    <li className="space-y-1 rounded-md border border-slate-200 p-3 text-sm">
      <div className="flex flex-wrap items-baseline gap-2">
        <span className="rounded bg-slate-100 px-1.5 py-0.5 text-xs font-medium">{statusLabel(f)}</span>
        {f.target && <code className="text-xs text-slate-500">{f.target}</code>}
        <span className="font-medium">{f.title}</span>
      </div>
      {f.source && <p className="text-xs text-slate-500">{f.source}</p>}
      {f.description && <p className="text-slate-600">{f.description}</p>}
      {f.remarks && <p className="text-slate-600">{f.remarks}</p>}
      {f.observations.length > 0 && (
        <ul aria-label={`Observations of ${f.title}`} className="mt-2 space-y-1 border-l border-slate-200 pl-3">
          {f.observations.map((o, i) => (
            <li key={i}>
              <span className="font-medium">{o.result ? `${o.result} · ` : ''}{o.title}</span>
              {o.description && <p className="break-words text-slate-600">{o.description}</p>}
              {o.evidence.map((e) => <p key={e} className="break-all text-xs text-slate-500">Evidence: {e}</p>)}
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}

// AssessmentViewer: the findings of an OSCAL assessment result with their observations.
export function AssessmentViewer({ assessment }: { assessment: Assessment }) {
  const [q, setQ] = useState('')
  const counts = new Map<string, number>()
  for (const f of assessment.findings) counts.set(statusLabel(f), (counts.get(statusLabel(f)) ?? 0) + 1)
  const shown = assessment.findings.filter((f) =>
    matches(q, [f.target, f.title, f.source, f.description, statusLabel(f), ...f.observations.flatMap((o) => [o.title, o.description])]))
  return (
    <>
      {assessment.title && <p className="text-sm font-medium">{assessment.title}</p>}
      <p className="text-sm text-slate-600">{[`${assessment.findings.length} requirements`, countLine(counts)].filter(Boolean).join(' · ')}</p>
      <FilterBox label="Filter requirements" value={q} onChange={setQ} />
      <ul aria-label="Requirements" className="space-y-2">
        {shown.map((f, i) => <FindingCard key={`${f.target ?? ''}-${i}`} f={f} />)}
      </ul>
    </>
  )
}
