import { Link } from '@tanstack/react-router'
import { displayLocation, type ApiError } from '../api/client'
import { identityString } from '../lib/contents'
import type { Level } from '../lib/attestations'
import type { ComponentSearch } from '../router'
import { ErrorPanel } from './ErrorPanel'
import { inspectSearch } from './InspectLabel'

export const linkClass = 'text-blue-700 underline'

// NotCollected: attestations are gathered for one version/location only; link to it, staying on view.
export function NotCollected({ name, error, view }: { name: string; error: ApiError; view: ComponentSearch['view'] }) {
  const first = (error.detail.collected as { version: string; repository: string }[] | undefined)?.[0]
  if (!first) return <ErrorPanel error={error} />
  return (
    <p className="text-sm text-slate-600">
      Attestations are not collected for this version/location. They are currently collected for{' '}
      <Link
        to="/components/$"
        params={{ _splat: name }}
        search={(s) => ({
          ...s,
          ...inspectSearch(first.version, first.repository),
          view,
        })}
        className={linkClass}
      >
        {first.version} at {displayLocation(first.repository)}
      </Link>{' '}
      only.
    </p>
  )
}

// subjectLabel names an attestation subject ("Component", "Resource image v1.0 (arch=amd64)").
export function subjectLabel(level: Level): string {
  const r = level.resource
  if (!r) return 'Component'
  const extra = identityString(r.extraIdentity)
  return `Resource ${r.name}${r.version ? ` ${r.version}` : ''}${extra ? ` (${extra})` : ''}`
}
