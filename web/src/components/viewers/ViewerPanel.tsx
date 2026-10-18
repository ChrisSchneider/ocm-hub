import { ApiError, attestationContentUrl, resourceUrl, type DocumentSource } from '../../api/client'
import { isHttpsUrl, type EntryViewer } from '../../lib/attestations'
import { ErrorPanel } from '../ErrorPanel'
import { ImageViewer } from './ImageViewer'
import { JsonModal } from './JsonModal'
import { SarifViewer } from './SarifViewer'
import { SbomViewer } from './SbomViewer'
import { PanelLinks, SidePanel, type PanelLink } from './SidePanel'
import { useAttestationJson, useDocument } from './useDocument'
import { VexViewer } from './VexViewer'

export interface ViewerScope { name: string; version: string; repository?: string }

export type ViewerTarget =
  | { kind: 'sbom' | 'vex' | 'sarif'; title: string; source: DocumentSource; originalUrl?: string }
  | { kind: 'raw'; title: string; attestation: string; originalUrl?: string }
  | { kind: 'image'; title: string; resource: string; extraIdentity?: Record<string, string> | null; digest: string }
  | { kind: 'json'; title: string; value: unknown }

// attestationTarget: the viewer target of an attestation document.
export const attestationTarget = (kind: EntryViewer, title: string, digest: string, originalUrl?: string): ViewerTarget =>
  kind === 'raw' ? { kind, title, attestation: digest, originalUrl } : { kind, title, source: { attestation: digest }, originalUrl }

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

function DocumentViewer({ scope, source, originalUrl }: { scope: ViewerScope; source: DocumentSource; originalUrl?: string }) {
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
      {doc.sbom ? <SbomViewer sbom={doc.sbom} />
        : doc.vex ? <VexViewer vex={doc.vex} />
          : doc.sarif ? <SarifViewer sarif={doc.sarif} />
            : <p className="text-sm text-slate-600">This document can't be shown here.</p>}
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

// ViewerPanel: SBOM, VEX, SARIF, raw attestation and image targets open in the side panel, JSON values in a modal.
export function ViewerPanel({ scope, target, onClose }: { scope: ViewerScope; target: ViewerTarget; onClose: () => void }) {
  if (target.kind === 'json') return <JsonModal title={target.title} value={target.value} onClose={onClose} />
  return (
    <SidePanel title={target.title} onClose={onClose}>
      {target.kind === 'image'
        ? <ImageViewer scope={scope} resource={target.resource} extraIdentity={target.extraIdentity} digest={target.digest} />
        : target.kind === 'raw'
          ? <RawViewer scope={scope} attestation={target.attestation} originalUrl={target.originalUrl} />
          : <DocumentViewer scope={scope} source={target.source} originalUrl={target.originalUrl} />}
    </SidePanel>
  )
}
