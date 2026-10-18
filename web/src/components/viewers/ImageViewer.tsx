import type { ReactNode } from 'react'
import { imageRawUrl, type ImageDetails } from '../../api/client'
import { formatBytes } from '../../lib/facts'
import { ErrorPanel } from '../ErrorPanel'
import { DocTable } from './DocTable'
import { PanelLinks } from './SidePanel'
import { useImageDetails } from './useDocument'
import type { ViewerScope } from './ViewerPanel'

const lines = (values: string[] | undefined) => (values?.length ? values.map((v, i) => <div key={`${i}:${v}`}>{v}</div>) : '—')

function fields(d: ImageDetails): [string, ReactNode][] {
  return [
    ['compressed size', formatBytes(d.size)],
    ['layers', String(d.layers.length)],
    ['created', d.created || '—'],
    ['platform', d.platform],
    ['user', d.user || '—'],
    ['entrypoint', d.entrypoint?.join(' ') || '—'],
    ['cmd', d.cmd?.join(' ') || '—'],
    ['workdir', d.workingDir || '—'],
    ['exposed ports', d.exposedPorts?.join(', ') || '—'],
    ['env', lines(d.env)],
    ['image labels', lines(Object.entries(d.labels ?? {}).map(([k, v]) => `${k}=${v}`))],
  ]
}

// ImageViewer: one platform of an image, like `docker inspect`.
export function ImageViewer({ scope, resource, extraIdentity, digest }: { scope: ViewerScope; resource: string; extraIdentity?: Record<string, string> | null; digest: string }) {
  const q = useImageDetails(scope, resource, extraIdentity, digest)
  const raw = (part: 'manifest' | 'config') => imageRawUrl(scope.name, scope.version, resource, extraIdentity, digest, part, scope.repository)
  const links = <PanelLinks links={[{ label: 'Raw manifest', href: raw('manifest') }, { label: 'Raw config', href: raw('config') }]} />
  if (q.isPending) return <p className="text-sm text-slate-500">Fetching document…</p>
  if (q.isError) return <><ErrorPanel error={q.error} />{links}</>
  const d = q.data
  return (
    <>
      <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
        {fields(d).map(([term, value]) => (
          <div key={term} className="contents">
            <dt className="text-slate-500">{term}</dt>
            <dd className="break-all font-mono">{value}</dd>
          </div>
        ))}
      </dl>
      <DocTable
        label="Layers"
        columns={['size', 'created by']}
        rows={d.layers.map((l, i) => ({ ...l, key: String(i) }))}
        getKey={(l) => l.key}
        cells={(l) => [formatBytes(l.size), l.createdBy || '—']}
      />
      {links}
    </>
  )
}
