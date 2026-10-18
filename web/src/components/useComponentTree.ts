import { useQuery } from '@tanstack/react-query'
import { getDescriptor, getDescriptorTree, type LocatedDescriptor } from '../api/client'

// The compact recursive tree of a component version; one unresolvable
// reference fails that call, so fall back to the root descriptor alone (its
// direct references then show as unloaded leaves). Shared by the body and the
// page header through the query cache: no extra requests.
export function useComponentTree(name: string, version: string, repository?: string) {
  const tree = useQuery({ queryKey: ['tree', name, version, repository ?? ''], queryFn: () => getDescriptorTree(name, version, repository) })
  const root = useQuery({
    queryKey: ['descriptor', name, version, repository ?? ''],
    queryFn: () => getDescriptor(name, version, repository),
    enabled: tree.isError,
  })
  const located: LocatedDescriptor[] | undefined = tree.data ?? (root.data ? [{ location: repository ?? '', descriptor: root.data }] : undefined)
  return { tree, root, located }
}
