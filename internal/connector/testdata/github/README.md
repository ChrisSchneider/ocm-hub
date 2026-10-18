These fixtures are synthetic. They contain no real data (organisation, component names, versions, dates and digests are all made up)
but follow the exact response shape of the GitHub Packages REST API for the fake org `acme-corp`.

Endpoints modelled:
- `GET /orgs/acme-corp/packages?package_type=container&per_page=100` (packages.json)
- `GET /orgs/acme-corp/packages/container/<url-escaped package name>/versions?per_page=100` (versions-*.json)

Fields are limited to what the connector parses: packages carry `name`, `package_type`, `updated_at`; versions carry `name`
(a fake `sha256:` digest), `created_at`, `updated_at` and `metadata` (`package_type`, `container.tags`). Timestamps use `2006-01-02T15:04:05Z`.

Structural cases covered:
- plain `component-descriptors/<name>` packages;
- nested descriptor packages under a repository sub path: one level (`ocm/component-descriptors/...`), two levels
  (`team-a/ocm/component-descriptors/...`) and one whose component name is itself host-like (`docs/component-descriptors/github.com/acme-corp/docs-site`);
- non-descriptor packages (`web`, `cli`) and look-alikes (`kubernetes/controller`) that must be ignored;
- a component with versions on two pages (`platform/gateway`): page 1 links to page 2 via `Link: rel="next"`
  in GitHub's `/organizations/<id>/...` form (fake id 90000001), as the real API does instead of echoing `/orgs/<name>/...`;
- untagged versions interleaved with tagged ones, tags like `1.4.0`, `1.5.0-rc.1`, `2.0.0`, and a version with several tags;
- package `updated_at` values on both sides of the delta `since` used by the test.

Comparing with the real API: inspect `gh api -i "<endpoint above>"` and check the shape matches.

User owners (`user:`): `GET /users/jane-doe/packages?...` (user-packages.json, page 2 via GitHub's
`/user/<id>/packages` Link form, fake id 90000002, user-packages-page2.json) and
`GET /users/jane-doe/packages/container/<pkg>/versions` (user-versions-*.json; the `tools/old`
package has no versions file and answers 200 `[]`).

Owner profiles (`Describe`): `GET /orgs/acme-corp` (org-acme-corp.json) and `GET /users/jane-doe`
(user-jane-doe.json, `name: null` like a user without a display name). Only `login`, `name` and
`avatar_url` are kept; the avatar ids are the fake owner ids above.

Packages carry `visibility`; `component-descriptors/acme.example/shop/backend` is `private`, all others `public`.

Enterprise (`testdata/github/enterprise/`, all synthetic): `POST /graphql` for enterprise `acme-enterprise`
(graphql-page1.json, then graphql-page2.json when the request carries `after` = `Y3Vyc29yOjE=`; the orgs are
deliberately not sorted: `acme-labs` before `acme-corp`), `GET /orgs/acme-corp/packages` (acme-corp-packages.json),
`GET /orgs/acme-labs/packages` (acme-labs-packages.json, an `internal` package) and a shared `versions.json`
for every `.../versions` request.
