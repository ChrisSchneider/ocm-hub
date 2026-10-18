import { rowClass } from './grid'

// PLACEHOLDERS: made-up rows showing how fused findings might look; not data.
const PLACEHOLDERS: [finding: string, severity: string, affected: string, sources: string, status: string][] = [
  ['CVE-XXXX-NNNN · openssl 3.x', 'High', 'alpine, python3', 'CVE scan, OpenVEX', 'Not affected (VEX)'],
  ['CVE-XXXX-NNNN · zlib 1.x', 'Critical', 'python3', 'CVE scan, CycloneDX SBOM', 'Open'],
  ['FIPS-003 · OpenSSL legacy provider installed', 'Warning', 'python3', 'FIPS scan (SARIF)', 'Open'],
  ['SBOM for every image', '—', 'docs', 'OSCAL assessment results', 'Not satisfied'],
]

// FindingsView: an outlook, not implemented. It will fuse the findings of all supply chain
// documents into one list; until then it explains that and shows greyed-out placeholders.
export function FindingsView() {
  return (
    <div className="space-y-3">
      <p className="text-sm text-slate-600">
        <span className="font-semibold">Outlook:</span> Findings will fuse what the supply chain documents report (CVE scans,
        VEX statements, SARIF results such as FIPS checks, OSCAL assessment results) into one list. Each finding appears once
        across resources and documents, VEX statements are applied to scanner results, and each finding links back to the
        documents it came from. Not implemented yet: the rows below are placeholders.
      </p>
      <div role="table" aria-label="Findings" aria-disabled className="select-none text-sm text-slate-400 opacity-60">
        <div role="row" className={`${rowClass('findings')} border-b border-slate-300 py-1 text-xs font-semibold uppercase tracking-wide`}>
          <span role="columnheader">Finding</span>
          <span role="columnheader">Severity</span>
          <span role="columnheader">Affected</span>
          <span role="columnheader">Sources</span>
          <span role="columnheader">Status</span>
        </div>
        {PLACEHOLDERS.map((cells, i) => (
          <div key={i} role="row" className={`${rowClass('findings')} border-b border-slate-100 py-1`}>
            {cells.map((c, j) => (
              <div key={j} role="cell" className={`truncate ${j === 0 ? 'font-mono text-xs' : 'text-xs'}`} title={c}>
                {c}
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}
