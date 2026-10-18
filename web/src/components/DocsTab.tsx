import { useState } from 'react'
import { resourceUrl, type OcmResource, type VersionOverview } from '../api/client'
import { OverviewGate } from './OverviewGate'
import { ResourceText } from './ResourceText'

type DocLink = VersionOverview['docs'][number]
const KIND_LABEL: Record<NonNullable<DocLink['kind']>, string> = {
  docs: 'Documentation', api: 'API reference', tutorial: 'Tutorials', issues: 'Issues', website: 'Website',
}

const docResources = (ov: VersionOverview): OcmResource[] => ov.resources.filter((r) => r.type === 'documentation')

// hasDocs: the version has documentation resources or docs links.
export const hasDocs = (ov: VersionOverview): boolean => ov.docs.length > 0 || docResources(ov).length > 0

// Groups in order of first appearance; links without kind under "Links".
function groupLinks(docs: DocLink[]): [string, DocLink[]][] {
  const groups = new Map<string, DocLink[]>()
  for (const d of docs) {
    const label = d.kind ? KIND_LABEL[d.kind] : 'Links'
    groups.set(label, [...(groups.get(label) ?? []), d])
  }
  return [...groups]
}

function pickerLabel(r: OcmResource, all: OcmResource[]): string {
  const collides = all.filter((x) => x.name === r.name).length > 1
  const extra = Object.values(r.extraIdentity ?? {})
  return collides && extra.length > 0 ? `${r.name} (${extra.join(', ')})` : r.name
}

function DocsBody({ ov }: { ov: VersionOverview }) {
  const rs = docResources(ov)
  const [i, setI] = useState(0)
  if (!hasDocs(ov)) return <p className="text-sm text-slate-600">This component version has no documentation.</p>
  const r = rs[Math.min(i, rs.length - 1)]
  return (
    <div className="flex flex-col gap-6 lg:flex-row">
      <div className="min-w-0 flex-1 space-y-3">
        {rs.length > 1 && (
          <div className="flex flex-wrap gap-1" role="group" aria-label="Documentation">
            {rs.map((x, j) => (
              <button key={j} type="button" aria-pressed={j === i} onClick={() => setI(j)}
                className={`rounded border px-2 py-0.5 text-sm ${j === i ? 'border-blue-700 bg-blue-50' : 'border-slate-300'}`}>
                {pickerLabel(x, rs)}
              </button>
            ))}
          </div>
        )}
        {r && <ResourceText url={resourceUrl(ov.name, ov.version, r.name, r.extraIdentity, ov.repository)} name={r.name} what="documentation" safeLinks />}
      </div>
      {ov.docs.length > 0 && (
        <aside className="w-full space-y-4 text-sm lg:w-72" aria-label="Documentation links">
          {groupLinks(ov.docs).map(([label, links]) => (
            <div key={label}>
              <h2 className="text-xs font-semibold uppercase text-slate-500">{label}</h2>
              <ul className="mt-1 space-y-0.5">
                {links.map((d) => (
                  <li key={d.url}>
                    <a href={d.url} target="_blank" rel="noreferrer" className="text-blue-700 underline">{d.title}</a>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </aside>
      )}
    </div>
  )
}

// Documentation resources of the component version (rendered) and its docs label links.
export function DocsTab({ name, version, repository, inspecting }: { name: string; version: string; repository?: string; inspecting: boolean }) {
  return (
    <OverviewGate name={name} version={version} repository={repository} inspecting={inspecting}>
      {(ov) => <DocsBody key={`${ov.name}:${ov.version}:${ov.repository}`} ov={ov} />}
    </OverviewGate>
  )
}
