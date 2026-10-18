import { useQuery } from '@tanstack/react-query'
import { getOverview } from '../api/client'

// One overview query per component version and location, shared by the
// Overview, Docs, Usage and Compliance tabs and the page tabs.
export const overviewQuery = (name: string, version: string, repository?: string) => ({
  queryKey: ['overview', name, version, repository ?? ''] as const,
  queryFn: () => getOverview(name, version, repository),
})

export const useOverview = (name: string, version: string, repository?: string) => useQuery(overviewQuery(name, version, repository))
