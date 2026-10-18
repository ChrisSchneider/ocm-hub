import { useQuery } from '@tanstack/react-query'
import { ApiError, getAttestations } from '../api/client'

export const attestationsQuery = (name: string, version: string, repository?: string) => ({
  queryKey: ['attestations', name, version, repository ?? ''] as const,
  queryFn: () => getAttestations(name, version, repository),
  retry: (count: number, err: Error) => !(err instanceof ApiError) && count < 2,
})

// Loaded independently of the overview: it has its own error states.
export const useAttestations = (name: string, version: string, repository?: string, opts?: { enabled?: boolean }) =>
  useQuery({ ...attestationsQuery(name, version, repository), enabled: opts?.enabled ?? true })
