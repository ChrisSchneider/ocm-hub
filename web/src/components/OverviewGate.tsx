import { useNavigate } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import type { VersionOverview } from '../api/client'
import { ErrorPanel } from './ErrorPanel'
import { clearInspectSearch } from './InspectLabel'
import { useOverview } from './useOverview'

// Loading and error states of the tabs that read the version overview.
export function OverviewGate({ name, version, repository, inspecting, children }: { name: string; version: string; repository?: string; inspecting: boolean; children: (ov: VersionOverview) => ReactNode }) {
  const navigate = useNavigate({ from: '/components/$' })
  const q = useOverview(name, version, repository)
  if (q.isPending) return <div className="h-24 animate-pulse rounded-lg bg-slate-100" aria-label="Loading" />
  if (q.isError) {
    return (
      <div className="space-y-2">
        <ErrorPanel error={q.error} />
        {inspecting && (
          <button type="button" className="text-blue-700 underline" onClick={() => void navigate({ search: clearInspectSearch })}>
            Switch to default
          </button>
        )}
      </div>
    )
  }
  return <>{children(q.data)}</>
}
