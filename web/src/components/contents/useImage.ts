import { useQuery } from '@tanstack/react-query'
import { ApiError, getImage } from '../../api/client'
import { identityString } from '../../lib/contents'
import type { ViewerScope } from '../viewers/ViewerPanel'

// useImage: the platforms of an image resource, fetched once enabled (first expand) and kept in the cache.
export const useImage = (scope: ViewerScope, resource: string, extraIdentity: Record<string, string> | null | undefined, enabled: boolean) =>
  useQuery({
    queryKey: ['image', scope.name, scope.version, scope.repository ?? '', resource, identityString(extraIdentity)],
    queryFn: () => getImage(scope.name, scope.version, resource, extraIdentity, scope.repository),
    retry: (count: number, err: Error) => !(err instanceof ApiError) && count < 2,
    enabled,
  })
