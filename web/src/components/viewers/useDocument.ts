import { useQuery } from '@tanstack/react-query'
import { ApiError, getAttestationJson, getDocument, getImageDetails, type DocumentSource } from '../../api/client'
import type { ViewerScope } from './ViewerPanel'

const retry = (count: number, err: unknown) => !(err instanceof ApiError) && count < 2

export const useDocument = ({ name, version, repository }: ViewerScope, source: DocumentSource) =>
  useQuery({
    queryKey: ['document', name, version, repository ?? '', source] as const,
    queryFn: () => getDocument(name, version, source, repository),
    retry,
  })

export const useAttestationJson = ({ name, version, repository }: ViewerScope, digest: string) =>
  useQuery({
    queryKey: ['attestation-json', name, version, repository ?? '', digest] as const,
    queryFn: () => getAttestationJson(name, version, digest, repository),
    retry,
  })

export const useImageDetails = ({ name, version, repository }: ViewerScope, resource: string, extraIdentity: Record<string, string> | null | undefined, digest: string) =>
  useQuery({
    queryKey: ['image-details', name, version, repository ?? '', resource, extraIdentity ?? null, digest] as const,
    queryFn: () => getImageDetails(name, version, resource, extraIdentity, digest, repository),
    retry,
  })
