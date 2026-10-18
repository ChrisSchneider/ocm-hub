import { useState } from 'react'
import type { components } from '../../api/schema'
import { plural } from '../../lib/facts'
import { DocTable, FilterBox, matches } from './DocTable'

type SBOM = components['schemas']['DocumentSBOM']

const distinct = (values: (string | undefined)[]) => new Set(values.filter((v) => v)).size

export function SbomViewer({ sbom }: { sbom: SBOM }) {
  const [q, setQ] = useState('')
  const all = sbom.packages.map((p, i) => ({ ...p, key: String(i) }))
  const rows = all.filter((p) => matches(q, [p.name, p.version, p.license, p.type]))
  return (
    <>
      <p className="text-sm text-slate-600">
        {[plural(all.length, 'package', 'packages'), plural(distinct(all.map((p) => p.license)), 'license', 'licenses'),
          plural(distinct(all.map((p) => p.type)), 'ecosystem', 'ecosystems')].join(' · ')}
      </p>
      <FilterBox label="Filter packages" value={q} onChange={setQ} />
      <DocTable
        label="Packages"
        columns={['package', 'version', 'license', 'type']}
        rows={rows}
        getKey={(p) => p.key}
        cells={(p) => [p.name, p.version ?? '—', p.license ?? '—', p.type ?? '—']}
      />
    </>
  )
}
