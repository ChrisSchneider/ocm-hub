import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Award, Check, Info } from 'lucide-react'
import type { ReactNode } from 'react'
import { ApiError, displayLocation, getVersions, type Component, type VersionOverview } from '../api/client'
import { contentsGroups } from '../lib/contentsSummary'
import { sourceLink } from '../lib/facts'
import { componentIcon } from '../lib/icon'
import { displayTitle } from '../lib/names'
import { collapseMinors } from '../lib/kubernetes'
import { overviewRows, storageLabel, type Row } from '../lib/overviewRows'
import { ComponentIcon } from './ComponentIcon'
import { hasDocs } from './DocsTab'
import { ErrorPanel } from './ErrorPanel'
import { inspectSearch, useDefaultView } from './InspectLabel'
import { linkClass, NotCollected } from './NotCollected'
import { OverviewGate } from './OverviewGate'
import { useAttestations } from './useAttestations'

const MAX_PARENTS = 10
// MAX_NAMES: resource names shown per type in the Resources pod.
const MAX_NAMES = 4
const grey = 'text-slate-400'
const chip = 'rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-700'

function Responsible({ r }: { r: VersionOverview['responsibles'][number] }) {
  const host = r.githubHostname ?? 'github.com'
  const link = (href: string, text: string) => (
    <a href={href} target="_blank" rel="noreferrer" className={linkClass}>
      {text}
    </a>
  )
  if (r.type === 'githubUser' && r.username) return link(`https://${host}/${r.username}`, `@${r.username}`)
  if (r.type === 'githubTeam' && r.teamname) {
    const [org, team] = r.teamname.split('/')
    return link(`https://${host}/orgs/${org}/teams/${team}`, r.teamname)
  }
  if (r.type === 'codeowners') return <span>CODEOWNERS of the source repository</span>
  return null
}

// commas joins inline items with ", ".
const commas = (items: React.ReactNode[]) => items.map((x, i) => <span key={i}>{i > 0 && ', '}{x}</span>)

// fromLabel: where a platform was seen ("from OCM and image").
const fromLabel = (from: VersionOverview['architectures'][number]['from']) =>
  `from ${[from.includes('ocm') && 'OCM', from.includes('image') && 'image'].filter(Boolean).join(' and ')}`

// Chips: a wrapping list of small chips.
function Chips({ label, children }: { label: string; children: ReactNode }) {
  return (
    <ul aria-label={label} className="mt-1 flex flex-wrap gap-1.5">
      {children}
    </ul>
  )
}

// Pod: a bordered box in the main column; empty ones are dashed and grey.
function Pod({ title, empty, aside, children }: { title: string; empty?: boolean; aside?: ReactNode; children: ReactNode }) {
  return (
    <section aria-label={title} className={`rounded-xl p-4 ${empty ? 'border border-dashed border-slate-300' : 'border border-slate-200 bg-white'}`}>
      <div className="flex items-baseline justify-between gap-3">
        <h3 className={`text-sm font-semibold ${empty ? grey : 'text-slate-700'}`}>{title}</h3>
        {aside}
      </div>
      <div className="mt-3 space-y-3 text-sm">{children}</div>
    </section>
  )
}

// ResourcesPod: one entry per resource type with its count and the first
// names, then the component references.
function ResourcesPod({ component, ov }: { component: Component; ov: VersionOverview }) {
  const groups = contentsGroups(ov.contents, ov.resources, ov.references)
  const total = groups.reduce((n, g) => n + g.count, 0)
  const all = (
    <Link to="/components/$" params={{ _splat: component.name }} search={(s) => ({ version: s.version, repository: s.repository, view: 'contents' })} className="text-xs text-blue-700 hover:underline">
      All {total} →
    </Link>
  )
  if (groups.length === 0)
    return (
      <Pod title="Resources" empty>
        <p className={grey}>None</p>
      </Pod>
    )
  return (
    <Pod title="Resources" aside={all}>
      {groups.map((g) => (
        <div key={g.label} data-group={g.label}>
          <div className="flex items-baseline justify-between">
            <span className="font-medium">{g.label}</span>
            <span className="text-xs text-slate-500">{g.count}</span>
          </div>
          {g.names.length > 0 && (
            <Chips label={g.label}>
              {g.names.slice(0, MAX_NAMES).map((n) => (
                <li key={n} className={`${chip} font-mono`}>
                  {n}
                </li>
              ))}
              {g.names.length > MAX_NAMES && <li className="py-0.5 text-xs text-slate-500">+{g.names.length - MAX_NAMES} more</li>}
            </Chips>
          )}
        </div>
      ))}
    </Pod>
  )
}

// CompatibilityPod: declared Kubernetes versions, distributions and storage,
// and the architectures it ships for. Dashed when nothing is declared.
function CompatibilityPod({ ov }: { ov: VersionOverview }) {
  const k = ov.kubernetes
  const versions = k ? collapseMinors(k.versions) || k.constraint : ''
  const storage = ov.storage ?? []
  const declared = !!versions || (k?.distributions.length ?? 0) > 0 || storage.length > 0
  const notRead = !ov.imagePlatformsRead && <span className={`text-xs ${grey}`}>image platforms not read</span>
  return (
    <Pod title="Compatibility" empty={!declared} aside={declared && <span className="text-xs text-slate-500">declared by the provider</span>}>
      {!declared && <p className={grey}>No Kubernetes versions, storage or other dependencies declared.</p>}
      {versions && (
        <div>
          <div className="font-medium">Kubernetes</div>
          <Chips label="Kubernetes">
            {versions.split(', ').map((v) => (
              <li key={v} className={`${chip} font-mono`}>
                {v}
              </li>
            ))}
          </Chips>
        </div>
      )}
      {k && k.distributions.length > 0 && (
        <div>
          <div className="font-medium">Distributions</div>
          <Chips label="Distributions">
            {k.distributions.map((d) => (
              <li key={d} className={chip}>
                {d}
              </li>
            ))}
          </Chips>
        </div>
      )}
      {storage.length > 0 && (
        <div>
          <div className="font-medium">Storage</div>
          <Chips label="Storage">
            {storage.map((n, i) => (
              <li key={i} title={n.purpose} className={chip}>
                {storageLabel(n)}
              </li>
            ))}
          </Chips>
        </div>
      )}
      <div>
        <div className={`font-medium ${declared ? '' : 'text-xs text-slate-500'}`}>Architectures</div>
        {ov.architectures.length > 0 ? (
          <Chips label="Architectures">
            {ov.architectures.map((a) => (
              <li key={a.platform} title={fromLabel(a.from)} className={`${chip} font-mono`}>
                {a.platform}
              </li>
            ))}
            {notRead && <li className="py-0.5">{notRead}</li>}
          </Chips>
        ) : (
          <p className={grey}>
            Not declared{notRead && ' · '}
            {notRead}
          </p>
        )}
      </div>
    </Pod>
  )
}

// Value: an evidence row's value; gaps and unknowns in grey.
function Value({ r, ov, pending }: { r: Row; ov: VersionOverview; pending: boolean }) {
  if (r.state === 'unknown') {
    if (pending) return <span className={grey}>…</span>
    return (
      <span className={grey} title="Attestations could not be loaded or a resource could not be checked">
        Not checked
      </span>
    )
  }
  if (r.state === 'missing') return <span className={grey}>{r.value}</span>
  switch (r.key) {
    case 'signature':
      return (
        <span className="inline-flex items-center gap-1.5">
          <span title="signed (not verified)" className={`inline-flex ${grey}`}>
            <Award aria-hidden size={16} />
          </span>
          {r.value}
        </span>
      )
    case 'slsa': {
      // The value ends with the source repository's label; link it.
      const src = sourceLink(ov.sources ?? [])
      if (!src || !r.value?.endsWith(src.label)) return <>{r.value}</>
      const head = r.value.slice(0, -src.label.length).replace(/ · $/, '')
      return (
        <>
          {head && <span>{head} · </span>}
          <a href={src.href} target="_blank" rel="noreferrer" className={`${linkClass} font-mono text-xs`}>
            {src.label}
          </a>
        </>
      )
    }
    default:
      return <>{r.value}</>
  }
}

// Why: an info icon whose tooltip says what the evidence is for (hover or focus).
function Why({ text }: { text: string }) {
  return (
    <span className="group relative inline-flex">
      <button type="button" aria-label={text} className="text-slate-300 hover:text-slate-500 focus:text-slate-500 focus:outline-none">
        <Info aria-hidden size={14} />
      </button>
      <span role="tooltip" className="pointer-events-none absolute top-5 left-1/2 z-10 hidden w-64 -translate-x-1/2 rounded-lg bg-slate-900 p-3 text-xs font-normal text-white shadow-lg group-focus-within:block group-hover:block">
        {text}
      </span>
    </span>
  )
}

// Evidence: the supply-chain list. Values say what is attached, never that it is good.
function Evidence({ rows, component, ov, pending }: { rows: Row[]; component: Component; ov: VersionOverview; pending: boolean }) {
  return (
    <ul aria-label="Supply-chain evidence" className="divide-y divide-slate-100 rounded-xl border border-slate-200 bg-white text-sm">
      {rows.map((r) => {
        const gap = r.state === 'missing'
        return (
          <li key={r.key} data-row={r.key} aria-label={r.label} className={`flex flex-wrap items-baseline gap-x-4 gap-y-1 px-4 py-3 ${gap ? 'bg-slate-50' : ''}`}>
            <span className={`flex w-full shrink-0 items-center gap-1.5 font-medium sm:w-36 ${gap ? grey : ''}`}>
              {r.label}
              {r.why && <Why text={r.why} />}
            </span>
            <span className="min-w-0 flex-1 text-slate-700">
              <Value r={r} ov={ov} pending={pending} />
            </span>
            {r.target && (
              <Link
                to="/components/$"
                params={{ _splat: component.name }}
                search={(s) => ({ version: s.version, repository: s.repository, view: r.target!.view })}
                hash={r.target.hash}
                aria-label={`Show ${r.label}`}
                className="shrink-0 text-xs text-slate-500 hover:text-blue-700"
              >
                View
              </Link>
            )}
          </li>
        )
      })}
    </ul>
  )
}

// Locations: every location of the component as a chip. The one shown is
// highlighted, the others holding this version switch to it, and those without
// it are dashed (all plain while it is not known which hold it).
function Locations({ component, ov, held }: { component: Component; ov: VersionOverview; held?: string[] }) {
  const navigate = useNavigate({ from: '/components/$' })
  const dflt = useDefaultView(component.name, component.defaultVersion)
  return (
    <ul className="flex flex-wrap gap-1.5" aria-label="Locations">
      {component.locations.map(({ repository: r }) => {
        const text = displayLocation(r)
        const chip = 'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-xs'
        if (r === ov.repository)
          return (
            <li key={r} className={`${chip} border-blue-300 bg-blue-50 text-blue-900`} title="Shown here">
              <Check aria-hidden size={12} />
              {text}
            </li>
          )
        if (held && !held.includes(r))
          return (
            <li key={r} className={`${chip} border-dashed border-slate-300 text-slate-400`} title={`${ov.version} is not at this location`}>
              {text}
            </li>
          )
        return (
          <li key={r}>
            <button
              type="button"
              disabled={!held}
              title={held ? `Show ${ov.version} from this location` : undefined}
              onClick={() => void navigate({ search: (s) => ({ ...s, ...inspectSearch(ov.version, r, dflt) }) })}
              className={`${chip} border-slate-300 bg-white text-slate-700 enabled:hover:border-slate-400 enabled:hover:bg-slate-50`}
            >
              {text}
            </button>
          </li>
        )
      })}
    </ul>
  )
}

// Fact: one sidebar entry; missing is shown in grey.
function Fact({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div>
      <dt className="text-xs text-slate-500">{term}</dt>
      <dd className="mt-0.5">{children}</dd>
    </div>
  )
}

// Sidebar: the way to Usage and Docs, then the version's facts. Right of the
// main column on wide screens, below it on narrow ones.
function Sidebar({ component, ov, held }: { component: Component; ov: VersionOverview; held?: string[] }) {
  const categories = ov.categories ?? []
  const button = 'flex-1 rounded-lg px-4 py-2 text-center text-sm font-medium'
  const notDeclared = <span className={grey}>Not declared</span>
  const parents = component.referencedBy.slice(0, MAX_PARENTS)
  return (
    <aside aria-label="Facts" className="space-y-5 text-sm lg:col-start-3 lg:row-start-1">
      <div className="flex gap-2">
        <Link to="/components/$" params={{ _splat: component.name }} search={(s) => ({ ...s, view: 'usage' })} className={`${button} bg-slate-900 text-white hover:bg-slate-700`}>
          Usage
        </Link>
        <Link
          to="/components/$"
          params={{ _splat: component.name }}
          search={(s) => ({ ...s, view: 'docs' })}
          title={hasDocs(ov) ? undefined : 'No documentation'}
          className={`${button} border ${hasDocs(ov) ? 'border-slate-300 hover:bg-slate-50' : 'border-slate-200 text-slate-400 hover:bg-slate-50'}`}
        >
          Docs
        </Link>
      </div>
      <dl className="space-y-3 rounded-xl border border-slate-200 bg-white p-4">
        <Fact term="Version">
          <span className="font-mono">{ov.version}</span>
        </Fact>
        <Fact term="Categories">
          {categories.length > 0 ? (
            <ul className="flex flex-wrap gap-1.5" aria-label="Categories">
              {categories.map((c) => (
                <li key={c} className="rounded-full border border-slate-200 bg-white px-2 py-0.5 text-xs font-medium text-slate-700">
                  {c}
                </li>
              ))}
            </ul>
          ) : (
            notDeclared
          )}
        </Fact>
        <Fact term="Responsibles">{ov.responsibles.length > 0 ? <>{commas(ov.responsibles.map((x, i) => <Responsible key={i} r={x} />))}</> : notDeclared}</Fact>
        <Fact term="Locations">
          <Locations component={component} ov={ov} held={held} />
        </Fact>
        <Fact term="Component">
          <span className="font-mono text-xs break-all">{component.name}</span>
        </Fact>
        <Fact term="Used by">
          {parents.length > 0 ? (
            <>
              {commas(
                parents.map((p) => (
                  <Link key={p.name} to="/components/$" params={{ _splat: p.name }} search={{}} className={linkClass}>
                    {displayTitle(p.title, p.name)}
                  </Link>
                )),
              )}
              {component.referencedByCount > MAX_PARENTS && <span className="text-slate-500"> +{component.referencedByCount - MAX_PARENTS} more</span>}
            </>
          ) : (
            <span className={grey}>None</span>
          )}
        </Fact>
      </dl>
    </aside>
  )
}

// Overview of one component version: who makes it and what it does, its
// contents and compatibility, the supply-chain evidence attached, and a
// sidebar of facts.
export function OverviewTab({ component, version, repository, inspecting }: { component: Component; version: string; repository?: string; inspecting: boolean }) {
  return (
    <OverviewGate name={component.name} version={version} repository={repository} inspecting={inspecting}>
      {(ov) => <OverviewBody component={component} ov={ov} version={version} repository={repository} />}
    </OverviewGate>
  )
}

function OverviewBody({ component, ov, version, repository }: { component: Component; ov: VersionOverview; version: string; repository?: string }) {
  const name = component.name
  const q = useAttestations(name, version, repository)
  // The locations holding this version, from the versions list narrowed to it
  // (undefined while loading, on error or for a non-semver version).
  const held = useQuery({
    queryKey: ['versions', name, 'exact', ov.version],
    queryFn: () => getVersions(name, { semverConstraint: `=${ov.version}`, includePrerelease: true }),
    retry: false,
  })
  const versionLocations = held.data?.versions.find((v) => v.version === ov.version)?.locations
  const rows = overviewRows(component, ov, q.data, q.isError)
  const title = displayTitle(ov.title || component.title, name)

  return (
    <div className="space-y-6 pt-4">
      {!component.latest && <p className="text-sm text-slate-600">No stable version yet — showing the newest pre-release.</p>}
      <section aria-label="About" className="flex items-start gap-4">
        <ComponentIcon icon={componentIcon({ ...component, icon: ov.icon ?? component.icon })} title={title} size={56} />
        <div className="min-w-0">
          <h2 className="text-3xl font-semibold tracking-tight">{title}</h2>
          <p className="mt-1 text-slate-500">
            {ov.provider ? (
              <>
                by <span className="font-medium text-slate-700">{ov.provider}</span>
              </>
            ) : (
              <span className={grey}>Provider not declared</span>
            )}
          </p>
        </div>
      </section>
      <div className="grid gap-10 lg:grid-cols-3">
        <div className="min-w-0 space-y-8 lg:col-span-2 lg:row-start-1">
          {ov.description && <p className="text-lg text-slate-700">{ov.description}</p>}
          <div className="grid gap-4 sm:grid-cols-2">
            <ResourcesPod component={component} ov={ov} />
            <CompatibilityPod ov={ov} />
          </div>
          <section aria-labelledby="evidence" className="space-y-2">
            <h3 id="evidence" className="text-sm font-semibold text-slate-700">
              Supply-chain evidence
            </h3>
            {q.error instanceof ApiError && q.error.code === 'NOT_COLLECTED' ? <NotCollected name={name} error={q.error} view={undefined} /> : q.error && <ErrorPanel error={q.error} />}
            <Evidence rows={rows.filter((r) => r.section === 'evidence')} component={component} ov={ov} pending={q.isPending} />
            <p className="text-xs text-slate-500">
              Shows what is attached; the hub verifies none of it. See{' '}
              <Link to="/components/$" params={{ _splat: name }} search={(s) => ({ ...s, view: 'usage' })} className={linkClass}>
                Usage
              </Link>{' '}
              for <code>ocm verify cv</code>.
            </p>
          </section>
        </div>
        <Sidebar component={component} ov={ov} held={versionLocations} />
      </div>
    </div>
  )
}
