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
- [ ] Print a final report, that includes number of synced components and any issues
      like not having a VEX URL in the documents.allow list

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
- [ ] CSAF VEX documents (e.g. Red Hat's `security.access.redhat.com/data/csaf/v2/vex/`
      feed, linked from the example's `vex` label): parse into VEX statements in
      `document.Parse`. Today a CSAF link is listed but answers `UNSUPPORTED_DOCUMENT`;
      a folder URL also needs resolving to the product's document.
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

## Compliance pipeline (standalone commands)
First prototype: FIPS step 1 (SBOM-only heuristics) as SARIF attached to the component,
and a BSI-like OSCAL assessment result (e.g. every image has an SBOM). Later:
- [ ] OCM verify (`ocm verify cv` / bindings) against allowed signers: Sigstore identity
      `github.com/chrisschneider` for now; `sigstore.ocmconfig` has no verifier policy yet.
- [ ] All OCI artifacts / container images signed (BSI SYS.1.6.A12), via cosign or
      `sigstore-go`.
- [ ] SBOM minimum fields with `sbomqs` incl. its compliance reports (BSI TR-03183-2,
      NTIA, ...)
- [ ] FIPS step 2: unpack the image filesystem and inspect it statically (`fips.so`,
      `openssl.cnf` activating the FIPS provider and `default_properties = fips=yes`; Go
      binaries via `debug/buildinfo`: boringcrypto / `GOFIPS140`, cgo + dynamic OpenSSL;
      static ELF binaries or bundled crypto symbols; libcrypto FIPS symbols)
- [ ] FIPS step 3: run the image (`openssl-fips-test`, HMAC-MD5 negative test), see
      https://edu.chainguard.dev/platform/fips/verify-fips/.
- [ ] Fuse SBOM + VEX, scan with grype, fail on untriaged critical / high CVEs.
- [ ] Helm chart in the example component; scan with `kubescape scan` +
      https://github.com/kubescape/regolibrary (or conftest / OPA).
- [ ] CIS Docker image checks with dockle (`CIS-DI-*`, SARIF output).
- [ ] Own rules as Rego over the collected evidence (embedded OPA)
- [ ] OSCAL `implementation-status: partial` ("teilweise") next to
      `status: satisfied | not-satisfied`; map to the BSI Grundschutz++ OSCAL catalog.
- [ ] Reference compliance catalogues as OCM components (label contract)
- [ ] Frameworks beyond BSI (CIS, NIST 800-53) in the control mapping.
- [ ] Configurable checks and thresholds (today: `--config` is only the `.ocmconfig`).

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
