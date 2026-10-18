import { useQuery } from '@tanstack/react-query'
import { getDescriptorYAML } from '../api/client'
import { CopyButton } from './CopyButton'
import { ErrorPanel } from './ErrorPanel'

// The component descriptor of the inspected version as YAML.
export function DescriptorTab({ name, version, repository }: { name: string; version: string; repository?: string }) {
  const yaml = useQuery({ queryKey: ['descriptor-yaml', name, version, repository ?? ''], queryFn: () => getDescriptorYAML(name, version, repository) })
  if (yaml.isError) return <ErrorPanel error={yaml.error} />
  if (!yaml.data) return <div className="h-24 animate-pulse rounded-lg bg-slate-100" aria-label="Loading" />
  return (
    <div className="relative">
      <pre className="overflow-auto rounded-md bg-slate-900 p-3 pr-10 text-xs text-slate-100">{yaml.data}</pre>
      <CopyButton text={yaml.data} label="Copy descriptor" tone="dark" className="absolute right-2 top-2" />
    </div>
  )
}
