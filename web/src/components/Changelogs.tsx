import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { getDescriptorTree, resourceUrl } from '../api/client'
import { changelogSources, type ChangelogSource } from '../lib/changelog'
import { ErrorPanel } from './ErrorPanel'
import { ResourceText } from './ResourceText'

function ChangelogBody({ source: s }: { source: ChangelogSource }) {
  const url = resourceUrl(s.component, s.version, s.resource.name, s.resource.extraIdentity, s.location)
  return <ResourceText url={url} name={s.resource.name} what="changelog" />
}

function SubBlock({ source: s }: { source: ChangelogSource }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="rounded border border-slate-200 p-2">
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="font-mono text-sm hover:underline">
        {s.component} {s.version}
      </button>
      {s.foreign && <span className="ml-2 rounded bg-slate-100 px-1.5 text-xs text-slate-600">other registry</span>}
      {open && <div className="mt-2"><ChangelogBody source={s} /></div>}
    </div>
  )
}

export function Changelogs({ name, version, repository }: { name: string; version: string; repository: string }) {
  const q = useQuery({ queryKey: ['changelogTree', name, version, repository], queryFn: () => getDescriptorTree(name, version, repository, { compact: false }) })
  if (q.isError) return <ErrorPanel error={q.error} />
  if (!q.data) return <p className="text-sm text-slate-500">Loading…</p>
  const sources = changelogSources(q.data)
  if (sources.length === 0) return <p className="text-sm text-slate-600">No changelog in this version or its references.</p>
  return (
    <div className="space-y-2">
      {sources.map((s) =>
        s.component === name && s.version === version ? (
          <ChangelogBody key={`${s.component}@${s.version}/${s.resource.name}`} source={s} />
        ) : (
          <SubBlock key={`${s.component}@${s.version}/${s.resource.name}@${s.location}`} source={s} />
        ),
      )}
    </div>
  )
}
