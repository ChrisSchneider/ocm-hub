import { useQuery } from '@tanstack/react-query'
import { ApiError, getSupplyChain } from '../api/client'

export const supplyChainQuery = (name: string, version: string, repository?: string) => ({
  queryKey: ['supply-chain', name, version, repository ?? ''] as const,
  queryFn: () => getSupplyChain(name, version, repository),
  retry: (count: number, err: Error) => !(err instanceof ApiError) && count < 2,
})

// Loaded independently of the overview: it has its own error states.
export const useSupplyChain = (name: string, version: string, repository?: string, opts?: { enabled?: boolean }) =>
  useQuery({ ...supplyChainQuery(name, version, repository), enabled: opts?.enabled ?? true })
