import type { ReactNode } from 'react'
import { VirtualList } from '../VirtualList'

const threshold = 100

// DocTable: a plain table up to 100 rows, above that only the visible rows
// (VirtualList with table roles).
export function DocTable<T>({ label, columns, rows, getKey, cells }: {
  label: string
  columns: string[]
  rows: T[]
  getKey: (row: T) => string
  cells: (row: T) => ReactNode[]
}) {
  const grid = { gridTemplateColumns: `repeat(${columns.length}, minmax(0, 1fr))` }
  if (rows.length > threshold) {
    return (
      <div className="text-sm">
        <div aria-hidden="true" className="grid gap-2 border-b border-slate-200 px-2 py-1 text-left font-medium text-slate-500" style={grid}>
          {columns.map((c) => <span key={c}>{c}</span>)}
        </div>
        <VirtualList
          items={rows}
          getKey={getKey}
          threshold={threshold}
          label={label}
          role="table"
          itemRole="row"
          renderRow={(row) => (
            <div className="grid gap-2 border-b border-slate-100 px-2 py-1" style={grid}>
              {cells(row).map((c, i) => <span role="cell" key={i} className="break-words">{c}</span>)}
            </div>
          )}
        />
      </div>
    )
  }
  return (
    <table aria-label={label} className="w-full table-fixed text-left text-sm">
      <thead>
        <tr className="border-b border-slate-200 text-slate-500">
          {columns.map((c) => <th key={c} className="px-2 py-1 font-medium">{c}</th>)}
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={getKey(row)} className="border-b border-slate-100 align-top">
            {cells(row).map((c, i) => <td key={i} className="break-words px-2 py-1">{c}</td>)}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export function FilterBox({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return (
    <input
      type="text"
      aria-label={label}
      placeholder={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="w-full rounded-md border border-slate-300 px-2 py-1 text-sm"
    />
  )
}

// matches: a case-insensitive substring match over the given fields.
export const matches = (q: string, fields: (string | undefined)[]) => {
  const needle = q.trim().toLowerCase()
  return !needle || fields.some((f) => f?.toLowerCase().includes(needle))
}

// countLine: "<key> <count>" per key, joined with " · ".
export const countLine = (counts: Map<string, number>) => [...counts].map(([k, n]) => `${k} ${n}`).join(' · ')
