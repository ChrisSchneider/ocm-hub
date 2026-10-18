# OCM Hub (Experiment)

A central library for all your [Open Component Model](https://ocm.software) (OCM) components.
Think of it as an app store for portable & signed software for air-gapped delivery.

## Quick start

Credentials: `cmd/sync` and the server read all registry credentials from the
`.ocmconfig` referenced by `hub.yaml`. The GitHub connector uses the ghcr.io entry
for the GitHub API and for reading descriptors (see `examples/.ocmconfig`).
Its password is a classic PAT with `read:packages` (+ `read:enterprise` for
`enterprise:` sources). With the GitHub CLI:

```sh
gh auth refresh -h github.com -s read:packages,read:enterprise
gh auth token
```

Build the hub index:

```sh
go run ./cmd/sync --config examples/hub.yaml          # delta (full on first run)
go run ./cmd/sync --config examples/hub.yaml --full   # also detects deletions, re-gathers attestations
```

Serve the hub (requires cgo for DuckDB):

```sh
pnpm --dir web build
go run ./cmd/server --config examples/hub.yaml
```

The web UI is then at http://localhost:8080/.

## Configuration (`hub.yaml`)

Full example: `examples/hub.yaml`.

| Key | Meaning |
|---|---|
| `listen` | Server address, e.g. `":8080"`. |
| `ocmconfig` | Path to the `.ocmconfig` with registry credentials. |
| `db.path` | DuckDB index file, relative to `hub.yaml`. |
| `timeouts.upstream` | Timeout for registry calls (descriptors, resources, attestation lookups). |
| `timeouts.shutdownGrace` | Graceful shutdown time of the server. |
| `cache.dir` | OCM blob cache directory (default: `<user cache dir>/ocm-server`). |
| `cache.blobs.maxEntries`, `maxBlobSize`, `remotePolicy` | Blob cache limits; `remotePolicy`: `IfNotPresent` or `Always`. |
| `versions.prereleaseMarkers` | Markers that make a version a pre-release (`alpha`, `rc`, …); other suffixes (`1.2-fix3`) are releases. |
| `catalog.kubernetesVersions` | Known Kubernetes minors the compatibility label is checked against. |
| `catalog.codeHosts` | Hosts whose first path segment is an owner (used to group component names). |
| `documents.allow` | URL patterns of label-linked documents the hub may fetch (below); empty: none are fetched. |
| `discovery[]` | Where `cmd/sync` finds components: `connector: github/v1` with one of `org`, `user` or `enterprise`; optional `visibility` (`public`, `internal`, `private`; default public) and `name` (title override). |
| `ui.enabled` | Serve the embedded web UI. |

## Supply chain

For each component's default version, `cmd/sync` records which supply chain documents exist
on the component and on its OCI resources: SLSA provenance, SBOM (SPDX, CycloneDX, …),
VEX, CVE scans, FIPS, image sources and others. It records each one's source:

- an embedded `sbom` resource,
- an OCI referrer (or the referrers tag schema),
- a cosign `.att` tag,
- a buildx attestation manifest,
- a `vex` label link.

The sync reads SBOM, VEX and scan (SARIF) attestation documents, capped at 32 MiB, to
summarise them (package, statement and finding counts); other documents are not read.
One document may carry several of these: a CycloneDX document's `vulnerabilities`
count as scan findings, the analysed ones also as VEX statements (states mapped to
OpenVEX statuses), next to its packages.
The hub fetches label-linked documents only from the URLs in the descriptor, never
from a URL the client sends, and only when a `documents.allow` pattern matches. Patterns
are [doublestar](https://github.com/bmatcuk/doublestar) globs over `host[:port]/path`
(no scheme, query or fragment; the host is case-insensitive): `*` stays within a segment,
so `*.example.com` matches `a.b.example.com` but not `example.com`, and `**` spans path
segments. Only https is fetched unless the pattern starts with `http://`. Every redirect
must match too. The list is the only gate: a listed URL may resolve to a private
address. An unlisted link is still listed as an attestation, without digest or summary,
and its content route answers `403 DOCUMENT_NOT_ALLOWED`.
A pattern needs a host and a path (`example.com/**`). The path is matched as sent:
URLs with dot segments (`.`, `..`, also `%2e` forms), an encoded `/` or `\` (`%2F`, `%5C`)
or a backslash are refused. A trailing dot on the host (`example.com.`), an explicit port
(`example.com:443`) and IPv6 literals match only when the pattern spells them out.

```yaml
documents:
  allow:
    - example.com/**/*.vex
    - "*.example.com/**/*.vex"
    - http://security.internal.corp/vex/*.json
```

Signatures are not verified. Results are stored
per component, version and location; the Supply chain view of the Contents tab lists every
document per resource with its source (OCI referrer, cosign tag, buildx attestation,
embedded SBOM resource or label link), and the Overview checklist sums them all up (SLSA
and sources, SBOM, VEX, scans) next to the descriptor's facts. The Findings view is an
outlook only: it will fuse the findings of all these documents into one list.

## Compliance checks (prototype)

Two standalone commands check a component version and attach their results to it as
unsigned in-toto attestations (OCI referrers of the component descriptor manifest).
They find SBOMs the same way the hub does. `--config` is an `.ocmconfig`; Sigstore
signing credentials in it are ignored.

```sh
REF=ghcr.io/chrisschneider//github.com/chrisschneider/example-with-opencode-images:1.1.0
go run ./cmd/fipscheck --config examples/components/sigstore.ocmconfig --attest $REF
go run ./cmd/assess    --config examples/components/sigstore.ocmconfig --attest $REF
```

- `cmd/fipscheck` checks each image's SBOM for signs that it is definitely *not*
  FIPS 140 compliant: OpenSSL 3 without a FIPS provider, OpenSSL 1.x, the legacy
  provider, bundled or other crypto libraries. It writes `fips.sarif` and, with
  `--attest`, attaches it; the hub shows it as a FIPS scan. A clean result proves nothing.
- `cmd/assess` writes an OSCAL 1.1.3 assessment result (`assessment-results.json`) and,
  with `--attest`, attaches it (the hub shows it as a compliance assessment: each
  requirement's status with its per-image observations). BSI-style
  requirements: every image has an SBOM (TR-03183-2), and FIPS cryptography,
  taken from the newest `fipscheck` attestation on the component. A requirement that
  only some images meet is `not-satisfied` with `implementation-status: partial`.

## Architecture

```text
 discovery sources ──► cmd/sync ──► DuckDB index ◄── cmd/server ──► REST API + web UI
 (GitHub orgs, …)        │          (catalog,          │
                         │           versions,         ├─ OCM bindings: descriptors,
                         ▼           attestations)     │  overviews, resources (live)
              OCI registries                           └─ OCI registries / allowed
              (descriptors, referrers, images)            links: documents, images (live)
```

- `cmd/sync` runs the discovery connectors (`internal/connector`) into a fresh copy of the
  index (`internal/index`) and swaps it in atomically. Per component it reads the default
  version's descriptor: overview, references, architectures (OCM identities and image
  platforms, `internal/image`) and attestations (`internal/attestation`, summarised by
  `internal/document`). It also records which versions ship a changelog.
- `cmd/server` serves the REST API (`internal/rest`, contract in `api/openapi.yaml`) and
  the embedded web UI (`web/`). Catalog, versions and attestation sets come from the index;
  descriptors, overviews, resources, image details and attestation documents are read
  live from OCM and the registries, label-linked documents only via `documents.allow`.
- Nothing is verified: the hub reports what is attached and where.

### Improvements

The hub should stay lean: an index, an API and a UI over libraries that do the work.
Candidates to move into separate Go modules:

- **Discovery** (`internal/connector`): "where are OCM components?" for GitHub, and
  later registries (zot, Harbor, Artifactory, GCR); aligned with the OCM Discovery API.
- **Attestation gathering** (`internal/attestation`): "what is attached to this
  component version / OCI subject?" (referrers, cosign tags, buildx, labels), usable from
  CLIs, controllers and CI.
  - **Image inspection** (`internal/image`): platforms and `docker inspect`-like details
    from the registry, nothing pulled.
  - **Document summaries** (`internal/document`): one normalised view over protobom,
    go-vex and go-sarif (plus CycloneDX vulnerabilities), with the counts.

## Development & Testing

```sh
go test ./...                 # unit, store and API tests; also integration tests against a local zot registry (needs Docker)
go test -short ./...          # without the Docker-based integration tests
pnpm --dir web test           # web UI (vitest)
pnpm --dir web lint           # TypeScript type check
pnpm --dir web build          # production build (embedded by the server)
```

Performance tests on a synthetic index (2000 components × 100 versions × 2 locations):

```sh
go test -tags perf -v -timeout 30m ./internal/perf/
```

After changing `api/openapi.yaml`, regenerate the Go and TypeScript types:

```sh
go generate ./internal/rest/gen/
pnpm --dir web gen
```

## API

All routes are GET (and HEAD) unless noted; full contract in `api/openapi.yaml`.
`{name}` is an OCM component name and contains `/` (send it percent-encoded or raw).

| Endpoint | Served from | What it returns |
|---|---|---|
| `/v1/components` | index | Catalog and search: `{components, next, total, facets}`; `facets.connectors[].groups` lists the owners below each source (e.g. an enterprise's orgs); `sources[].groups` does the same on `/v1/components/{name}`; `facets.replication` counts `single` and `replicated` components. Query: `q` (name/title substring), `location` (repeatable), `connector` (repeatable source key such as `github:enterprise:neonephos`, or an owner key such as `github:org:open-component-model`), `provider`, `category` (repeatable), `architecture` (repeatable, e.g. `linux/arm64`), `includePrerelease`, `updatedSince`, `topLevel`, `replication=single\|replicated` (one location, or two or more), `sort=relevance\|updated\|name`, `limit` (≤ 200), `cursor`. |
| `/v1/components/{name}` | index | Overview: latest version, title, description, icon, provider, categories, every location with its latest version, and `sources` (the discovery sources that found it). |
| `/v1/components/{name}/versions` | index | Versions, newest published first, each with its locations: `{name, versions, next}`. Query: `includePrerelease`, `semverConstraint`, `location`, `limit` (≤ 1000), `cursor`. |
| `/v1/components/{name}/descriptors/{version}` | OCM | The component descriptor (YAML, or JSON via `Accept`). `latest` redirects (307) to the newest stable version. `recursive=-1` returns `[{location, descriptor}]` for the whole reference tree; `compact=true` trims it. |
| `/v1/components/{name}/overview/{version}` | OCM | Overview of one version (labels, signed, contents, v2 resources and signatures), `latest` redirects; `repository` pins the location. |
| `/v1/components/{name}/supply-chain/{version}` | index | Supply chain documents of the component and its OCI resources stored for exactly that version and `repository`. Without `repository` it redirects (307) to the version's location; `latest` redirects; nothing stored answers `404 NOT_COLLECTED` (listing what was collected). |
| `/v1/components/{name}/supply-chain/{version}/{digest}` | index, then registry or link | One stored supply chain document (in-toto statements unwrapped to their predicate), as a download; `as=document` answers it parsed (`{format, sbom, vex, scan}`, at least one part set). Same `repository`/`latest` redirects as the set route. A digest not in the set answers `404 NOT_FOUND` without fetching; a label link that no longer matches the recorded digest answers `409 DOCUMENT_CHANGED`, one no longer on `documents.allow` `403 DOCUMENT_NOT_ALLOWED`; an unparseable document `422 UNSUPPORTED_DOCUMENT`; over 32 MiB `422 DOCUMENT_TOO_LARGE`. Nothing is verified. |
| `/v1/components/{name}/images/{version}/{resource}` | OCM, then registry | The platforms of an OCI image resource, read from its registry: `{repository, digest, mediaType, platforms: [{platform, digest}]}` (index children with os and architecture, or a single manifest as one platform, `platform` empty when its config lacks os or architecture). Extra identity via `extraIdentity.<key>=<value>`; `repository`/`latest` as on resources. A resource that is not an OCI image answers `422 NOT_OCI`; an unreadable registry `502 UPSTREAM_ERROR`. |
| `/v1/components/{name}/images/{version}/{resource}/{digest}` | OCM, then registry | One platform of the image (manifest and config summarised: size, created, user, entrypoint, env, labels, layers). `raw=manifest\|config` sends that JSON. A digest that is not one of the image's platforms answers `404 NOT_FOUND`. |
| `/v1/components/{name}/resources/{version}/{resource}` | OCM | Streams a resource; extra identity via `extraIdentity.<key>=<value>`. `as=document` answers the resource parsed as an SBOM, VEX and/or scan document (same errors as above). |
| `POST /v1/discovery` | – | Selector-based discovery aligned with OCM `Discovery`; not implemented yet (501). |
| `/healthz` | – | Liveness. |

## Labels

Labels the hub reads, and the value each must carry. A versioned label accepts its
version or none (the OCM CLI drops label versions); an invalid label is skipped with a
warning in the sync log. A component carrying all of them: `examples/components/`.

### Hub labels (`catalog.chrisschneider.dev/…`)

| Label | On | Value |
|---|---|---|
| `title` | component | String; catalog card and page title (fallback: component name). |
| `description` | component | String. |
| `icon` | component | `https://` URL, `data:` URL, or a named icon `lucide/<name>`. |
| `categories` | component | List of strings; catalog filter. |
| `compatibility` (v1) | component | `{kubernetes, storage}`, at least one of them. `kubernetes`: `{constraint, distributions}`; `constraint` is a semver range (required), e.g. `">=1.28 <1.34, !=1.30"`; `distributions` a list such as `[gardener, eks, gke]`. `storage`: the volumes it needs, a list of `{accessMode, volumeMode, purpose}`; `accessMode` is `RWO`, `RWX`, `ROX` or `RWOP` (or `ReadWriteOnce`, `ReadWriteMany`, `ReadOnlyMany`, `ReadWriteOncePod`), `volumeMode` `Filesystem` (default) or `Block`, `purpose` optional text. |
| `docs` (v1) | component | List of `{title, url, kind}`; `url` must be `https`; `kind`: `docs`, `api`, `tutorial`, `issues` or `website`. |
| `vex` (v1) | component or resource | `{url}`: a VEX document published outside the component (e.g. a vendor's CSAF feed or `vex.json`). Not an OCI attestation: its 👁 in the Labels view opens the VEX part of the document, fetched and shown only when the URL matches `documents.allow`; it counts for the Overview's VEX row. |

### OCM ecosystem labels

| Label | On | Value |
|---|---|---|
| `odg.ocm.software/responsibles` (v1) | component | List of `{type: githubUser, username}`, `{type: githubTeam, teamname: org/team}` or `{type: codeowners}`; optional `github_hostname`. |
| `security.ocm.software/risk-profile` (v1) | component | `network_exposure` (`private`, `protected`, `public`), `authentication_enforced` (bool), `user_interaction` (`operator`, `end-user`), `confidentiality_requirement`, `integrity_requirement`, `availability_requirement` (`none`, `low`, `high`), `comment`. |
| `ocm.software/artifact-references` (v1alpha1) | `sbom` resource | List of `{identity: {name, version?, …extraIdentity}}`: the resource this SBOM describes ([ocm-spec](https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/06-conventions.md)). Without it, the SBOM belongs to the component. |
