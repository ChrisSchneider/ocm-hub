// ROW: the column template of every findings row.
const ROW = 'grid grid-cols-[minmax(0,1.3fr)_8rem_minmax(0,1fr)_minmax(0,1.2fr)] items-center gap-x-3 px-2'

// PLACEHOLDERS: made-up rows showing how fused findings might look; not data. Rating is what
// the report itself says; Affected is what it is about (a package of a resource, a resource, the
// whole component); Source names the exact documents it came from.
const PLACEHOLDERS: [finding: string, rating: string, affected: string, source: string][] = [
  ['CVE-XXXX-NNNN · zlib 1.x', 'Critical', 'python3 → zlib 1.x', 'python3 · trivy.sarif.json'],
  ['CVE-XXXX-NNNN · openssl 3.x', 'High', 'alpine → openssl 3.x, python3 → openssl 3.x', 'alpine · grype.cdx.json, python3 · trivy.sarif.json'],
  ['FIPS-003 · OpenSSL legacy provider installed', 'Warning', 'python3 → openssl 3.x', 'python3 · fipscheck.sarif.json'],
  ['SBOM for every image', 'Not satisfied', 'whole component', 'assessment-results.oscal.json'],
]

// FindingsView: the Findings page tab, an outlook, not implemented. It will fuse the findings of all supply chain
// documents into one list; until then it explains that and shows greyed-out placeholders.
export function FindingsView() {
  return (
    <div className="space-y-3">
      <p className="text-sm text-slate-600">
        <span className="font-semibold">Outlook:</span> Findings will fuse what the supply chain documents report (CVE scans,
        SARIF results such as FIPS checks, OSCAL assessment results) into one list. Each finding appears once with the rating
        its report gives, what it affects (a package of a resource, a resource, or the whole component) and the exact documents
        it came from. CVEs that a VEX statement marks as not affecting are left out. Not implemented yet: the rows below are
        placeholders.
      </p>
      <p className="text-sm text-slate-600">
        <span className="font-semibold">Required checks:</span> Policies will be able to require certain check results: the
        top-level OSCAL assessment results such as assessment-results.oscal.json. These in turn check that the downstream
        checks (an SBOM for every image, a FIPS check, ...) are present and pass, so requiring them covers the whole chain.
        A component whose required assessment is missing or not satisfied is flagged.
      </p>
      <div role="table" aria-label="Findings" aria-disabled className="select-none text-sm text-slate-400 opacity-60">
        <div role="row" className={`${ROW} border-b border-slate-300 py-1 text-xs font-semibold uppercase tracking-wide`}>
          <span role="columnheader">Finding</span>
          <span role="columnheader">Rating</span>
          <span role="columnheader">Affected</span>
          <span role="columnheader">Source</span>
        </div>
        {PLACEHOLDERS.map((cells, i) => (
          <div key={i} role="row" className={`${ROW} border-b border-slate-100 py-1`}>
            {cells.map((c, j) => (
              <div key={j} role="cell" className={`truncate ${j === 0 || j === 3 ? 'font-mono text-xs' : 'text-xs'}`} title={c}>
                {c}
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}
