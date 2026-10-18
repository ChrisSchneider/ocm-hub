# TODOs

Open items first, grouped by area; finished work at the end.

## Discovery API
- [ ] Implement `POST /v1/discovery` (currently a `501` stub): `componentSelector` /
      `resourceSelector` with `matchIdentity` + `matchLabels`, `extract.byComponents` /
      `byResources` with dot paths; response `components` xor `extracted`.
      Aligned with upstream `Discovery` (`delivery.ocm.software/v1alpha1`).
- [ ] CEL `expression` in selectors and extraction (incl. `semverCheck`).

## Index & Sync
- [ ] Store labels and resources (and descriptors) of the default version, so search,
      filter and discovery can run against the index (references are stored already).
- [ ] In-server sync scheduling (today: run `cmd/sync` by hand or cron).

## Connectors
- [ ] `artifactory/v1`.
- [ ] `gcr/v1`.
- [ ] `zot/v1`, `quay/v1`, `harbor/v1`.
- [ ] `github/v1` delta: track each package's `updated_at` (and id) in the index instead of
      one global `since` (robust against clock skew and half-failed runs).
- [ ] `github/v1` delta: conditional requests — store the `ETag` per page URL (packages list
      and `/versions`) and send `If-None-Match`; a `304` costs no rate limit. Full syncs
      must ignore stored ETags (they need the complete list to detect deletions).
- [ ] GitHub App: push notifications on changed or new packages (update without polling)
      and a native way to add components from GitHub instead of manual config.

## Attestations
- [ ] Gather attestations for other versions and locations (today: default version at
      its overview location only).
- [ ] GitHub attestation API (attestations stored only at GitHub, not in the registry).
- [ ] Switch to the `artifactref/v1alpha1` binding once released (today: local lookup,
      and labels without version are accepted because bindings v0.0.8 drop it).
- [ ] Docker Hardened Images predicate types for STIG, secrets and virus scans
      (today listed as "Other").
- [ ] Deep browsing into components (e.g. open an SBOM).
- [ ] SLSA provenance viewer (today: raw JSON of the predicate). Parse it in
      `document.Parse` with the entry's predicate type as hint (the predicate alone has
      none), for v0.2 (buildx, older GitHub generators) and v1: builder id, build type,
      level; source / invocation (v1 `externalParameters`, v0.2 `invocation.configSource`);
      materials / resolved dependencies as uri + digest table (https links only); run
      details (invocation id, started / finished); raw JSON as fallback. A small variant
      for verification summaries (verifier, resource, result, verified levels).
      Large buildx `mode=max` provenance hits `document.MaxSize`.

## Compliance checks
- [ ] Policy engine for checks (Rego | CEL | Go | WASM?), based on
      https://container.gov.de/docs/referenzen/container-hardening-checklist/
- [ ] Checks: VEX attestation present; all critical & high CVEs rated; licence;
      SBOM; FIPS; SLSA provenance; sources; secrets scan; virus scan.
- [ ] Signature matches an allowed certificate or sigstore identity (own check).
      Use `github.com/sigstore/sigstore-go` to verify Sigstore bundles (signature,
      certificate chain to a TUF trusted root, Rekor / timestamp, identity policy,
      subject digest); bundles come from OCI referrers and the GitHub attestation API.
      Legacy cosign `.sig`/`.att` tags are not bundles and need separate handling.
- [ ] Coverage matrix / checklist: a component-level SLSA + VEX may cover all contents.

## Upgrades & Compatibility
- [ ] Upgrade path visualization ("upgradeable from" semver expression).
- [ ] Dependencies on other components and their versions (e.g. Kubernetes update before
      Ceph, or only compatible with certain cert-manager versions).
- [ ] Bundling conflicts: two components depend on different cert-manager versions —
      which one to bundle?

## Wizards & Synthetic Components
- [ ] Builders similar to Talos Image Factory.

## Other
- [ ] Detect differences between components with the same identity and version but
      different digests across registries.
- [ ] Stats during / at end of sync process
