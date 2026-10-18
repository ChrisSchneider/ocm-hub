import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { OcmDescriptor } from '../api/ocm'
import { contentItems, identityString, typeCounts, type ContentItem } from '../lib/contents'
import { childName, groupItems, isImage, type View } from '../lib/contentsRows'
import { describedResource } from '../lib/sbom'
import { AttestationsView } from './contents/AttestationsView'
import { ContentsView } from './contents/ContentsView'
import type { Line } from './contents/grid'
import { LabelsView } from './contents/LabelsView'
import { PullDialog } from './PullDialog'
import { attestationsQuery, useAttestations } from './useAttestations'
import { ViewerPanel, type ViewerTarget } from './viewers/ViewerPanel'

const SOURCE = '\0source'
const VIEWS: { id: View; label: string }[] = [
  { id: 'contents', label: 'Contents' },
  { id: 'labels', label: 'Labels' },
  { id: 'attestations', label: 'Attestations' },
]

const pill = (on: boolean) => `rounded px-2 py-0.5 ${on ? 'bg-slate-800 text-white' : 'bg-slate-100'}`

// ContentsList: the sources and resources of a descriptor in one of three views. Resources
// differing only in extraIdentity fold into an expandable group; an SBOM with an artifact
// reference sits right under the resource it describes. The attestation set is loaded only for
// the Attestations view or the first VEX label action.
export function ContentsList({ descriptor, repository, query, view, onView }: { descriptor: OcmDescriptor; repository?: string; query?: string; view: View; onView: (v: View) => void }) {
  const items = contentItems(descriptor)
  const { name, version } = descriptor.component
  const [type, setType] = useState<string>()
  const [pull, setPull] = useState<string>()
  const [viewer, setViewer] = useState<ViewerTarget>()
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const [labelsNeedSet, setLabelsNeedSet] = useState(false)
  const attestations = useAttestations(name, version, repository, { enabled: view === 'attestations' || labelsNeedSet })
  const queryClient = useQueryClient()

  const byType = !type ? items : type === SOURCE ? items.filter((i) => i.kind === 'source') : items.filter((i) => i.kind === 'resource' && (i.element.type ?? 'unknown') === type)
  const q = query?.trim().toLowerCase()
  const filtered = q ? byType.filter(({ element: e }) => [e.name, e.type, e.version, identityString(e.extraIdentity)].some((s) => s?.toLowerCase().includes(q))) : byType
  const resources = filtered.filter((i) => i.kind === 'resource').map((i) => i.element)
  const describes = new Map<string, string>() // sbom item key -> described resource name
  const under = new Map<object, ContentItem[]>() // described element -> its SBOM items
  for (const i of filtered) {
    if (i.kind !== 'resource' || i.element.type !== 'sbom') continue
    const target = describedResource(i.element, resources)
    if (!target || target.type === 'sbom') continue // SBOMs of SBOMs (chains, cycles) stay in place
    describes.set(i.key, target.name)
    under.set(target, [...(under.get(target) ?? []), i])
  }
  const withSboms = (item: ContentItem, depth: 0 | 1, label: string): Line[] => [
    { kind: 'item', key: item.key, item, depth, label },
    ...(view !== 'labels' && item.kind === 'resource' && isImage(item.element.type) && expanded.has(item.key) ? [{ kind: 'platforms', key: `${item.key}/platforms`, item, depth: depth === 0 ? 1 : 2 } as Line] : []),
    ...(under.get(item.element) ?? []).map((s): Line => ({ kind: 'item', key: s.key, item: s, depth: depth === 0 ? 1 : 2, label: s.element.name, note: `SBOM of ${describes.get(s.key)}` })),
  ]
  const lines = groupItems(filtered.filter((i) => !describes.has(i.key))).flatMap((r): Line[] =>
    r.kind === 'group'
      ? [{ kind: 'group', key: r.key, group: r }, ...(expanded.has(r.key) ? r.children.flatMap((c) => withSboms(c, 1, childName(c.element.extraIdentity))) : [])]
      : withSboms(r, 0, r.element.name),
  )
  const toggle = (key: string) =>
    setExpanded((s) => {
      const next = new Set(s)
      if (!next.delete(key)) next.add(key)
      return next
    })
  const loadSet = async () => {
    setLabelsNeedSet(true)
    try {
      return await queryClient.fetchQuery(attestationsQuery(name, version, repository))
    } catch {
      return undefined
    }
  }
  const unfiltered = !type && !q
  const hasSources = items.some((i) => i.kind === 'source')
  const scope = { name, version, repository }
  const common = { lines, expanded, onToggle: toggle, onViewer: setViewer }

  if (items.length === 0) return <p className="text-sm text-slate-500">No resources or sources.</p>
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-1 text-xs">
        <button type="button" aria-pressed={!type} onClick={() => setType(undefined)} className={pill(!type)}>
          all {items.length}
        </button>
        {hasSources && (
          <button type="button" aria-pressed={type === SOURCE} onClick={() => setType(SOURCE)} className={pill(type === SOURCE)}>
            source {items.filter((i) => i.kind === 'source').length}
          </button>
        )}
        {typeCounts(items).map(([t, n]) => (
          <button key={t} type="button" aria-pressed={type === t} onClick={() => setType(t)} className={pill(type === t)}>
            {t} {n}
          </button>
        ))}
        <div role="radiogroup" aria-label="Contents view" className="ml-auto inline-flex overflow-hidden rounded-md border border-slate-300 text-sm">
          {VIEWS.map((v, i) => (
            <button
              key={v.id}
              type="button"
              role="radio"
              aria-checked={view === v.id}
              onClick={() => onView(v.id)}
              className={`cursor-pointer px-3 py-0.5 ${i > 0 ? 'border-l border-slate-300' : ''} ${view === v.id ? 'bg-slate-800 text-white' : 'bg-white text-slate-700'}`}
            >
              {v.label}
            </button>
          ))}
        </div>
      </div>
      {pull && <PullDialog reference={pull} onClose={() => setPull(undefined)} />}
      {viewer && <ViewerPanel scope={scope} target={viewer} onClose={() => setViewer(undefined)} />}
      {view === 'labels' ? (
        <LabelsView
          {...common}
          componentLabels={unfiltered ? (descriptor.component.labels ?? []) : undefined}
          name={name}
          version={version}
          repository={repository}
          set={attestations.data}
          settled={attestations.isSuccess || attestations.isError}
          loadSet={loadSet}
        />
      ) : view === 'attestations' ? (
        <AttestationsView {...common} showComponent={unfiltered} scope={scope} set={attestations.data} pending={attestations.isPending} error={attestations.error} />
      ) : (
        <ContentsView {...common} scope={scope} onPull={setPull} />
      )}
      {lines.length === 0 && !unfiltered && <p className="text-sm text-slate-500">No match in this component.</p>}
    </div>
  )
}
