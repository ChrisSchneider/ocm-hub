import { useState } from 'react'
import type { components } from '../../api/schema'
import { countLine, DocTable, FilterBox, matches } from './DocTable'

type VEX = components['schemas']['DocumentVEX']

export function VexViewer({ vex }: { vex: VEX }) {
  const [q, setQ] = useState('')
  const all = vex.statements.map((s, i) => ({ ...s, key: String(i), product: s.products.join(', '), note: [s.justification, s.action].filter((v) => v).join(' / ') }))
  const counts = new Map<string, number>()
  for (const s of all) counts.set(s.status, (counts.get(s.status) ?? 0) + 1)
  const rows = all.filter((s) => matches(q, [s.vulnerability, s.product, s.status, s.note]))
  return (
    <>
      <p className="text-sm text-slate-600">{countLine(counts)}</p>
      <FilterBox label="Filter statements" value={q} onChange={setQ} />
      <DocTable
        label="Statements"
        columns={['vulnerability', 'product', 'status', 'justification / action']}
        rows={rows}
        getKey={(s) => s.key}
        cells={(s) => [s.vulnerability, s.product || '—', s.status, s.note || '—']}
      />
    </>
  )
}
