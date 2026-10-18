import { useState } from 'react'
import type { components } from '../../api/schema'
import { countLine, DocTable, FilterBox, matches } from './DocTable'

type SARIF = components['schemas']['DocumentSARIF']

const order = ['critical', 'high', 'medium', 'low']

// severityCounts: critical, high, medium, low, then other severity words in
// document order, then "none" for findings without one.
function severityCounts(findings: SARIF['findings']): Map<string, number> {
  const raw = new Map<string, number>()
  for (const f of findings) raw.set(f.severity || 'none', (raw.get(f.severity || 'none') ?? 0) + 1)
  const keys = [...order.filter((k) => raw.has(k)), ...[...raw.keys()].filter((k) => !order.includes(k) && k !== 'none'), ...(raw.has('none') ? ['none'] : [])]
  return new Map(keys.map((k) => [k, raw.get(k)!]))
}

export function SarifViewer({ sarif }: { sarif: SARIF }) {
  const [q, setQ] = useState('')
  const all = sarif.findings.map((f, i) => ({ ...f, key: String(i) }))
  const rows = all.filter((f) => matches(q, [f.rule, f.severity, f.location, f.message]))
  return (
    <>
      <p className="text-sm text-slate-600">{`${sarif.tool} · ${all.length} findings`}</p>
      {all.length > 0 && <p className="text-sm text-slate-600">{countLine(severityCounts(sarif.findings))}</p>}
      <FilterBox label="Filter findings" value={q} onChange={setQ} />
      <DocTable
        label="Findings"
        columns={['rule', 'severity', 'location', 'message']}
        rows={rows}
        getKey={(f) => f.key}
        cells={(f) => [f.rule, f.severity || '—', f.location || '—', f.message]}
      />
    </>
  )
}
