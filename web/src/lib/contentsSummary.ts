const NAMES: Record<string, [string, string]> = {
  ociImage: ['OCI image', 'OCI images'],
  helmChart: ['Helm chart', 'Helm charts'],
  changelog: ['changelog', 'changelogs'],
  sbom: ['SBOM', 'SBOMs'],
}

const plural = (n: number, [one, many]: [string, string]) => `${n} ${n === 1 ? one : many}`

// summaryItems: one entry per resource type, most frequent first, then the
// references; zero counts are left out ("2 OCI images", "1 changelog", "3 references").
export function summaryItems(c: { resources: { type: string; count: number }[]; references: number }): string[] {
  const items = [...c.resources]
    .filter((r) => r.count > 0)
    .sort((a, b) => b.count - a.count || a.type.localeCompare(b.type))
    .map(({ type, count }) => plural(count, NAMES[type] ?? [type, type]))
  if (c.references > 0) items.push(plural(c.references, ['reference', 'references']))
  return items
}

export interface ContentsGroup {
  // label: the type's name for the count ("OCI images"); references have no type.
  label: string
  count: number
  // names: the distinct resource names of the type, in descriptor order.
  names: string[]
}

const capital = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

// contentsGroups: one group per resource type in summaryItems order, then the
// component references.
export function contentsGroups(
  c: { resources: { type: string; count: number }[]; references: number },
  resources: { name: string; type?: string }[],
  references: { name: string }[] = [],
): ContentsGroup[] {
  const groups = [...c.resources]
    .filter((r) => r.count > 0)
    .sort((a, b) => b.count - a.count || a.type.localeCompare(b.type))
    .map(({ type, count }) => {
      const [one, many] = NAMES[type] ?? [type, type]
      return { label: capital(count === 1 ? one : many), count, names: [...new Set(resources.filter((r) => r.type === type).map((r) => r.name))] }
    })
  if (c.references > 0) groups.push({ label: c.references === 1 ? 'Reference' : 'References', count: c.references, names: [...new Set(references.map((r) => r.name))] })
  return groups
}
