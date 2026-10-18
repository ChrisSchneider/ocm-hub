import { useQuery } from '@tanstack/react-query'
import { Link, useParams, useSearch } from '@tanstack/react-router'
import { ApiError, getComponent } from '../api/client'
import { ComponentBody } from '../components/ComponentBody'
import { DescriptorTab } from '../components/DescriptorTab'
import { FindingsView } from '../components/contents/FindingsView'
import { DocsTab, hasDocs } from '../components/DocsTab'
import { LocationsBlock } from '../components/LocationsBlock'
import { ErrorPanel } from '../components/ErrorPanel'
import { OverviewTab } from '../components/OverviewTab'
import { PageTabs } from '../components/PageTabs'
import { UsageTab } from '../components/UsageTab'
import { VersionsTab } from '../components/VersionsTab'
import { overviewQuery } from '../components/useOverview'

export function ComponentPage() {
  const { _splat: name = '' } = useParams({ from: '/components/$' })
  const search = useSearch({ from: '/components/$' })
  const component = useQuery({ queryKey: ['component', name], queryFn: () => getComponent(name) })
  const version = search.version ?? component.data?.defaultVersion ?? ''
  // same key as the tabs: no extra request
  const ov = useQuery({ ...overviewQuery(name, version, search.repository), enabled: !!version })

  if (component.isPending) return <div className="h-24 animate-pulse rounded-lg bg-slate-100" aria-label="Loading" />
  if (component.isError) {
    if (component.error instanceof ApiError && component.error.status === 404) {
      return (
        <div className="space-y-2">
          <p>Component not found: {name}</p>
          <Link to="/" search={{}} className="text-blue-700 underline">
            Back to the catalog
          </Link>
        </div>
      )
    }
    return <ErrorPanel error={component.error} />
  }
  const o = component.data

  const inspecting = !!(search.version || search.repository)
  const body = () => {
    switch (search.view) {
      case 'contents':
      case 'labels':
      case 'supply-chain':
        return <ComponentBody key={`${name}:${version}:${search.repository ?? ''}`} name={name} version={version} search={search} />
      case 'findings':
        return <FindingsView />
      case 'descriptor':
        return <DescriptorTab name={name} version={version} repository={search.repository} />
      case 'versions':
        return <VersionsTab name={name} component={o} search={search} />
      case 'replication':
        return <LocationsBlock component={o} />
      case 'usage':
        return <UsageTab name={name} version={version} repository={search.repository} inspecting={inspecting} />
      case 'docs':
        return <DocsTab name={name} version={version} repository={search.repository} inspecting={inspecting} />
      default:
        return <OverviewTab component={o} version={version} repository={search.repository} inspecting={inspecting} />
    }
  }

  return (
    <div className="space-y-4">
      <PageTabs view={search.view} noDocs={ov.data && !hasDocs(ov.data)} />
      {body()}
    </div>
  )
}
