import type { LocatedDescriptor } from '../api/client'
import type { OcmElement } from '../api/ocm'

export interface ChangelogSource {
  component: string
  version: string
  location: string
  foreign: boolean // read from another location than the root
  resource: OcmElement
}

export function changelogSources(located: LocatedDescriptor[]): ChangelogSource[] {
  const root = located[0]?.location
  return located.flatMap(({ location, descriptor }) =>
    (descriptor.component.resources ?? [])
      .filter((r) => r.type === 'changelog')
      .map((resource) => ({ component: descriptor.component.name, version: descriptor.component.version, location, foreign: location !== root, resource })),
  )
}
