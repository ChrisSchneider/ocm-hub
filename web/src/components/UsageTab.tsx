import { useState } from 'react'
import { ocmRef } from '../api/ocm'
import type { VersionOverview } from '../api/client'
import { accessConfig, ctfArchive, ctfDownloadCommand, ctfUploadCommand, downloadCommand, sortResources, transferCommand, verifyConfig } from '../lib/usage'
import { CodeBlock } from './CodeBlock'
import { OverviewGate } from './OverviewGate'

const DOCS = {
  access: 'https://ocm.software/docs/concepts/credential-system/',
  inspect: 'https://ocm.software/docs/reference/ocm-cli/ocm-get-component-version/',
  verify: 'https://ocm.software/docs/how-to/sign-and-verify/verify-component-versions/',
  download: 'https://ocm.software/docs/how-to/download-resources-from-component-versions/',
  transfer: 'https://ocm.software/docs/concepts/transfer-and-transport/',
}

function Section({ title, docs, children }: { title: string; docs: string; children: React.ReactNode }) {
  return (
    <section className="space-y-2">
      <div className="flex items-baseline gap-3">
        <h2 className="text-lg font-semibold">{title}</h2>
        <a href={docs} target="_blank" rel="noreferrer" className="text-sm text-blue-700 underline">
          Docs
        </a>
      </div>
      {children}
    </section>
  )
}

function Download({ ov, ocmref }: { ov: VersionOverview; ocmref: string }) {
  const rs = sortResources(ov.resources)
  const [i, setI] = useState(0)
  const r = rs[Math.min(i, rs.length - 1)]
  return (
    <Section title="Download a resource" docs={DOCS.download}>
      {rs.length > 1 && (
        <label className="flex items-center gap-2 text-sm">
          Resource
          <select aria-label="Resource" value={i} onChange={(e) => setI(Number(e.target.value))} className="rounded border border-slate-300 px-2 py-1">
            {rs.map((x, j) => (
              <option key={j} value={j}>{`${x.name} · ${x.type} · ${x.version}`}</option>
            ))}
          </select>
        </label>
      )}
      <CodeBlock text={downloadCommand(ocmref, r)} label="Copy command" />
    </Section>
  )
}

// Copy-ready OCM v2 CLI commands for the inspected component version.
export function UsageTab({ name, version, repository, inspecting }: { name: string; version: string; repository?: string; inspecting: boolean }) {
  return (
    <OverviewGate name={name} version={version} repository={repository} inspecting={inspecting}>
      {(ov) => {
        const ref = ocmRef(ov.repository, ov.name, ov.version)
        return (
          <div className="space-y-6">
            <Section title="Configure access" docs={DOCS.access}>
              <p className="text-sm text-slate-600">
                Add to your <code>.ocmconfig</code>. Not needed for public locations.
              </p>
              <CodeBlock text={accessConfig(ov.repository)} label="Copy config" />
            </Section>
            <Section title="Inspect" docs={DOCS.inspect}>
              <CodeBlock text={`ocm get cv ${ref}`} label="Copy command" />
              <CodeBlock text={`ocm get cv ${ref} -o widetree --recursive`} label="Copy command" />
            </Section>
            {ov.signatures.length > 0 && (
              <Section title="Verify" docs={DOCS.verify}>
                <CodeBlock text={`ocm verify cv ${ref}`} label="Copy command" />
                <p className="text-sm text-slate-600">
                  Public keys, one entry per signature, in your <code>.ocmconfig</code>:
                </p>
                <CodeBlock text={verifyConfig(ov.signatures)} label="Copy config" />
              </Section>
            )}
            {ov.resources.length > 0 && <Download key={`${ov.name}:${ov.version}:${ov.repository}`} ov={ov} ocmref={ref} />}
            <Section title="Transfer: Registry to Registry" docs={DOCS.transfer}>
              <CodeBlock text={transferCommand(ref)} label="Copy command" />
            </Section>
            <Section title="Transfer: Air-Gap (CTF)" docs={DOCS.transfer}>
              <p className="text-sm text-slate-600">Download into a local CTF archive, with all resources and referenced components:</p>
              <CodeBlock text={ctfDownloadCommand(ref, ctfArchive(ov.name, ov.version))} label="Copy command" />
              <p className="text-sm text-slate-600">Move the archive, then upload it into your registry:</p>
              <CodeBlock text={ctfUploadCommand(ctfArchive(ov.name, ov.version), ov.name, ov.version)} label="Copy command" />
            </Section>
          </div>
        )
      }}
    </OverviewGate>
  )
}
