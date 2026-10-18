import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { displayLocation } from '../api/client'
import { overviewQuery } from './useOverview'

// The default a component page shows without ?version= and ?repository=.
export interface DefaultView {
  version: string
  repository?: string // where the default version is read from; undefined until known
}

// useDefaultView: the default version and the location it is read from (the
// default overview's repository; shared with the Overview tab's request).
export function useDefaultView(name: string, defaultVersion?: string): DefaultView | undefined {
  const ov = useQuery({ ...overviewQuery(name, defaultVersion ?? '', undefined), enabled: !!defaultVersion })
  return defaultVersion ? { version: defaultVersion, repository: ov.data?.repository } : undefined
}

// isDefaultView reports whether version (and an optional pinned location) is what the page shows by default.
export const isDefaultView = (version: string, repository: string | undefined, d?: DefaultView): boolean =>
  !!d && version === d.version && (!repository || repository === d.repository)

// Search params that pin a version (and location) for inspection; the
// default version at its default location leaves them out (clean URL).
export function inspectSearch(version: string, repository?: string, d?: DefaultView) {
  if (isDefaultView(version, repository, d)) return { version: undefined, repository: undefined, select: undefined, selectRepository: undefined }
  return { version, repository, select: undefined, selectRepository: undefined }
}

// Search update that leaves inspect mode (back to the latest version).
export const clearInspectSearch = <S extends object>(s: S) => ({ ...s, version: undefined, repository: undefined, select: undefined, selectRepository: undefined })

// VersionTag shows the selected version and the location it is read from.
// Away from the default (a pinned version or location) it is highlighted and
// offers to switch back.
export function VersionTag({ version, repository, isDefault }: { version: string; repository?: string; isDefault: boolean }) {
  const navigate = useNavigate({ from: '/components/$' })
  return (
    <div
      role="status"
      aria-label="Selected version"
      data-default={isDefault}
      className={`flex items-center gap-2 rounded-md px-2.5 py-1 text-sm ${isDefault ? 'bg-slate-100 text-slate-700' : 'bg-amber-50 text-amber-900'}`}
    >
      <span className={`rounded px-1.5 font-mono text-xs ${isDefault ? 'bg-white' : 'bg-amber-200'}`}>{version}</span>
      {repository && <span className="font-mono text-xs opacity-80">{displayLocation(repository)}</span>}
      {!isDefault && (
        <button type="button" className="underline" onClick={() => void navigate({ search: clearInspectSearch })}>
          Switch to default
        </button>
      )}
    </div>
  )
}
