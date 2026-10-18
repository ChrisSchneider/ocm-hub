import { ApiError, attestationContentUrl, resourceUrl, type DocumentSource } from '../../api/client'
import { useState } from 'react'
import type { components } from '../../api/schema'
import { isHttpsUrl, type DocumentPart, type EntryViewer } from '../../lib/attestations'
import { ErrorPanel } from '../ErrorPanel'
import { ImageViewer } from './ImageViewer'
import { JsonModal } from './JsonModal'
import { SbomViewer } from './SbomViewer'
import { ScanViewer } from './ScanViewer'
import { PanelLinks, SidePanel, type PanelLink } from './SidePanel'
import { useAttestationJson, useDocument } from './useDocument'
import { VexViewer } from './VexViewer'

export interface ViewerScope { name: string; version: string; repository?: string }

export type ViewerTarget =
  // part: shown first when the document carries several parts.
  | { kind: 'document'; title: string; source: DocumentSource; part?: DocumentPart; originalUrl?: string }
  | { kind: 'raw'; title: string; attestation: string; originalUrl?: string }
  | { kind: 'image'; title: string; resource: string; extraIdentity?: Record<string, string> | null; digest: string }
  | { kind: 'json'; title: string; value: unknown }

// attestationTarget: the viewer target of an attestation document.
export const attestationTarget = (kind: EntryViewer, title: string, digest: string, originalUrl?: string, part?: DocumentPart): ViewerTarget =>
  kind === 'raw' ? { kind, title, attestation: digest, originalUrl } : { kind, title, source: { attestation: digest }, part, originalUrl }

// original links an https document URL; other URLs are shown as text by OriginalText.
const original = (href: string | undefined): PanelLink[] => (href && isHttpsUrl(href) ? [{ label: 'Original document', href, external: true }] : [])

function OriginalText({ url }: { url?: string }) {
  return url && !isHttpsUrl(url) ? <p className="break-all text-xs text-slate-500">{url}</p> : null
}

// DocumentError: the panel body when a document can't be fetched or shown.
function DocumentError({ error, links, originalUrl }: { error: unknown; links: PanelLink[]; originalUrl?: string }) {
  const code = error instanceof ApiError ? error.code : ''
  if (code === 'DOCUMENT_CHANGED') {
    const url = (error as ApiError).detail.url
    return (
      <>
        <p className="text-sm text-slate-600">The linked document changed since the last sync.</p>
        {typeof url === 'string' && <><PanelLinks links={original(url)} /><OriginalText url={url} /></>}
      </>
    )
  }
  if (code === 'DOCUMENT_NOT_ALLOWED') {
    // The hub refuses to fetch it: only the original link is of use.
    const detail = (error as ApiError).detail.url
    const url = originalUrl ?? (typeof detail === 'string' ? detail : undefined)
    return <><p className="text-sm text-slate-600">This document can't be shown here.</p><PanelLinks links={original(url)} /><OriginalText url={url} /></>
  }
  if (code === 'UNSUPPORTED_DOCUMENT' || code === 'DOCUMENT_TOO_LARGE') {
    return <><p className="text-sm text-slate-600">This document can't be shown here.</p><PanelLinks links={links} /><OriginalText url={originalUrl} /></>
  }
  return <><ErrorPanel error={error} /><PanelLinks links={links} /><OriginalText url={originalUrl} /></>
}

const PART_LABELS: Record<DocumentPart, string> = { sbom: 'Packages', vex: 'VEX', scan: 'Scan results' }

// DocumentParts: the parts a document carries; with several, a switch between them that
// starts at part (else the first).
function DocumentParts({ doc, part }: { doc: components['schemas']['Document']; part?: DocumentPart }) {
  const parts = (['sbom', 'vex', 'scan'] as const).filter((k) => doc[k])
  const [chosen, setChosen] = useState<DocumentPart | undefined>(part)
  const shown = chosen && parts.includes(chosen) ? chosen : parts[0]
  if (!shown) return <p className="text-sm text-slate-600">This document can't be shown here.</p>
  return (
    <>
      {parts.length > 1 && (
        <div role="tablist" aria-label="Document parts" className="flex gap-1 text-sm">
          {parts.map((k) => (
            <button key={k} type="button" role="tab" aria-selected={k === shown} onClick={() => setChosen(k)}
              className={`cursor-pointer rounded px-2 py-0.5 ${k === shown ? 'bg-slate-800 text-white' : 'bg-slate-100 hover:bg-slate-200'}`}>
              {PART_LABELS[k]}
            </button>
          ))}
        </div>
      )}
      {shown === 'sbom' ? <SbomViewer sbom={doc.sbom!} /> : shown === 'vex' ? <VexViewer vex={doc.vex!} /> : <ScanViewer scan={doc.scan!} />}
    </>
  )
}

function DocumentViewer({ scope, source, part, originalUrl }: { scope: ViewerScope; source: DocumentSource; part?: DocumentPart; originalUrl?: string }) {
  const q = useDocument(scope, source)
  const { name, version, repository } = scope
  const links: PanelLink[] = [
    'attestation' in source
      ? { label: 'Raw document', href: attestationContentUrl(name, version, source.attestation, repository) }
      : { label: 'Download', href: resourceUrl(name, version, source.resource, source.extraIdentity, repository) },
    ...original(originalUrl),
  ]
  if (q.isPending) return <p className="text-sm text-slate-500">Fetching document…</p>
  if (q.isError) return <DocumentError error={q.error} links={links} originalUrl={originalUrl} />
  const doc = q.data
  return (
    <>
      <p className="text-xs text-slate-500">{doc.format}</p>
      <DocumentParts doc={doc} part={part} />
      <PanelLinks links={links} />
      <OriginalText url={originalUrl} />
    </>
  )
}

// RawViewer: an attestation document the hub doesn't parse, as pretty-printed JSON.
function RawViewer({ scope, attestation, originalUrl }: { scope: ViewerScope; attestation: string; originalUrl?: string }) {
  const q = useAttestationJson(scope, attestation)
  const links: PanelLink[] = [{ label: 'Raw document', href: attestationContentUrl(scope.name, scope.version, attestation, scope.repository) }, ...original(originalUrl)]
  if (q.isPending) return <p className="text-sm text-slate-500">Fetching document…</p>
  if (q.isError) return <DocumentError error={q.error} links={links} originalUrl={originalUrl} />
  return (
    <>
      <pre aria-label="Document" className="overflow-auto rounded bg-slate-900 p-3 font-mono text-xs text-slate-100">{JSON.stringify(q.data, null, 2)}</pre>
      <PanelLinks links={links} />
      <OriginalText url={originalUrl} />
    </>
  )
}

// ViewerPanel: parsed documents (packages, VEX, scan results), raw attestation and image targets open in the side panel, JSON values in a modal.
export function ViewerPanel({ scope, target, onClose }: { scope: ViewerScope; target: ViewerTarget; onClose: () => void }) {
  if (target.kind === 'json') return <JsonModal title={target.title} value={target.value} onClose={onClose} />
  return (
    <SidePanel title={target.title} onClose={onClose}>
      {target.kind === 'image'
        ? <ImageViewer scope={scope} resource={target.resource} extraIdentity={target.extraIdentity} digest={target.digest} />
        : target.kind === 'raw'
          ? <RawViewer scope={scope} attestation={target.attestation} originalUrl={target.originalUrl} />
          : <DocumentViewer scope={scope} source={target.source} part={target.part} originalUrl={target.originalUrl} />}
    </SidePanel>
  )
}
