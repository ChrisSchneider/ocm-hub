// Generated from api/openapi.yaml by `pnpm gen`. Do not edit.

export interface paths {
    "/healthz": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getHealth"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v1/": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getBase"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v1/components": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listComponents"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head: operations["headComponents"];
        patch?: never;
        trace?: never;
    };
    "/v1/components/{name}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getComponent"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head: operations["headComponent"];
        patch?: never;
        trace?: never;
    };
    "/v1/components/{name}/versions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listVersions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head: operations["headVersions"];
        patch?: never;
        trace?: never;
    };
    "/v1/components/{name}/descriptors/{version}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getDescriptor"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head: operations["headDescriptor"];
        patch?: never;
        trace?: never;
    };
    "/v1/components/{name}/overview/{version}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * @description Overview of one component version, read live from OCM (labels, signed,
         *     contents). `latest` redirects (307) to the concrete version.
         */
        get: operations["getVersionOverview"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head: operations["headVersionOverview"];
        patch?: never;
        trace?: never;
    };
    "/v1/components/{name}/attestations/{version}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * @description Attestations (SBOM, SLSA, VEX, ...) found on one component version and its
         *     OCI resources, as gathered by a sync (index only, no OCM call). Each sync
         *     gathers each component's default version at its overview location; sets are
         *     kept per version and location and looked up exactly at `?repository=`, which
         *     a 200 therefore requires. Without it the route redirects (307) to the same
         *     version with `?repository=` set to the location the other version routes
         *     use; `latest` redirects (307) once to the concrete version, adding that
         *     location if absent. Nothing stored at the location answers `404
         *     NOT_COLLECTED` with `detail.collected` listing what exists. Nothing is
         *     verified.
         */
        get: operations["getAttestations"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head: operations["headAttestations"];
        patch?: never;
        trace?: never;
    };
    "/v1/components/{name}/attestations/{version}/{digest}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * @description The document of one attestation of a stored set, found by its `digest`
         *     (index lookup, then read from the registry or, for a label link, fetched
         *     from the link, which must still hash to the digest the sync recorded).
         *     In-toto statements are unwrapped to their predicate. `?repository=` and
         *     `latest` behave as on `/attestations/{version}` (307). By default the bytes
         *     are sent as an attachment; `as=document` answers the parsed `Document`.
         *     A digest not in the set, an embedded SBOM (a resource) or an entry
         *     without content answer `404 NOT_FOUND`, with nothing fetched. A label
         *     link no longer on the allow list answers `403 DOCUMENT_NOT_ALLOWED`,
         *     with nothing fetched. Nothing is verified.
         */
        get: operations["getAttestationContent"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head: operations["headAttestationContent"];
        patch?: never;
        trace?: never;
    };
    "/v1/components/{name}/resources/{version}/{resource}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * @description Streams the resource content. Extra identity attributes are passed as
         *     `extraIdentity.<key>=<value>` query parameters (e.g. `extraIdentity.arch=amd64`).
         *     `as=document` reads the resource (at most 32 MiB) and answers the parsed
         *     `Document` as `application/json` instead.
         */
        get: operations["getResource"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head: operations["headResource"];
        patch?: never;
        trace?: never;
    };
    "/v1/components/{name}/images/{version}/{resource}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * @description The platforms of the OCI image a resource names, read from its registry
         *     (the children of an index, skipping attestation manifests, or the platform
         *     of a single manifest). Extra identity attributes are passed as
         *     `extraIdentity.<key>=<value>` query parameters, like on `/resources`.
         *     `?repository=` and `latest` behave as there (`latest` answers 307). A
         *     resource that is not an OCI image answers `422 NOT_OCI`; a registry that
         *     cannot be read `502 UPSTREAM_ERROR`. Nothing is verified.
         */
        get: operations["getImageIndex"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head: operations["headImageIndex"];
        patch?: never;
        trace?: never;
    };
    "/v1/components/{name}/images/{version}/{resource}/{digest}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * @description One platform of an image: its manifest and config summarised. `digest` must
         *     be one of the platform digests of `/images/{version}/{resource}`, otherwise
         *     `404 NOT_FOUND`. With `raw=manifest` or `raw=config` the stored JSON is sent
         *     instead, with its own media type. Other behaviour as on the platforms route.
         */
        get: operations["getImageDetails"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head: operations["headImageDetails"];
        patch?: never;
        trace?: never;
    };
    "/v1/discovery": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** @description Not implemented yet (always 501). Mirrors the `Discovery` resource of delivery.ocm.software/v1alpha1: selectors over components and resources, and either raw v2 descriptors or extracted records. */
        post: operations["discover"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        ErrorResponse: {
            errors: components["schemas"]["ErrorItem"][];
        };
        /**
         * @description `UNSUPPORTED_DOCUMENT` (422): not a recognised SBOM, VEX or scan (SARIF, CycloneDX) document.
         *     `DOCUMENT_TOO_LARGE` (422): over 32 MiB. `DOCUMENT_CHANGED` (409): a label
         *     link no longer serves the document the sync recorded (`detail.url`).
         *     `DOCUMENT_NOT_ALLOWED` (403): a label link's URL is not on the hub's
         *     `documents.allow` list (`detail.url`); nothing is fetched.
         *     `NOT_OCI` (422): the resource is not an OCI image. On the image routes
         *     `NOT_FOUND` also means the digest is not a platform of the image.
         * @enum {string}
         */
        ErrorCode: "NAME_INVALID" | "REPOSITORY_INVALID" | "REPOSITORY_NOT_ALLOWED" | "PARAMETER_INVALID" | "COMPONENT_UNKNOWN" | "VERSION_UNKNOWN" | "RESOURCE_UNKNOWN" | "NOT_FOUND" | "METHOD_NOT_ALLOWED" | "NOT_ACCEPTABLE" | "RESOURCE_AMBIGUOUS" | "INTERNAL_ERROR" | "ACCESS_TYPE_UNSUPPORTED" | "NOT_IMPLEMENTED" | "UPSTREAM_ERROR" | "UPSTREAM_TIMEOUT" | "INDEX_UNAVAILABLE" | "NOT_COLLECTED" | "UNSUPPORTED_DOCUMENT" | "DOCUMENT_TOO_LARGE" | "DOCUMENT_CHANGED" | "DOCUMENT_NOT_ALLOWED" | "NOT_OCI";
        ResourceIdentity: {
            name: string;
            version?: string;
            extraIdentity?: {
                [key: string]: string;
            };
        };
        AttestationEntry: {
            /** @enum {string} */
            kind: "slsa" | "sbom" | "vex" | "cve" | "fips" | "stig" | "secrets" | "virus" | "sources" | "other";
            /** @description e.g. `CycloneDX 1.6`. */
            format?: string;
            /** @description Raw predicate */
            type?: string;
            /** @enum {string} */
            how: "embedded" | "referrer" | "cosignTag" | "buildx" | "labelLink";
            platform?: string;
            /**
             * @description What covers the attestation; nothing is verified.
             * @enum {string}
             */
            signed: "signed" | "component" | "no";
            digest?: string;
            resource?: components["schemas"]["ResourceIdentity"];
            /** @description Link from a label. */
            url?: string;
            /** @description SLSA build level a slsa entry claims (from a verification summary or a known builder); not verified. */
            buildLevel?: number;
            /** @description SLSA provenance builder id. */
            builder?: string;
            /** @description Label name a labelLink entry came from. */
            label?: string;
            summary?: components["schemas"]["AttestationSummary"];
        };
        /** @description Counts read from the attestation document; zero counts are omitted. */
        AttestationSummary: {
            format: string;
            tool?: string;
            /** @description What `importantFindings` counts */
            importantBasis?: string;
            packages?: number;
            statements?: number;
            affected?: number;
            findings?: number;
            importantFindings?: number;
        };
        /** @description The component (no `resource`) or one OCI resource. */
        AttestationLevel: {
            resource?: components["schemas"]["ResourceIdentity"];
            /** @enum {string} */
            status: "ok" | "failed" | "notOci";
            error?: string;
            entries: components["schemas"]["AttestationEntry"][];
        };
        AttestationSet: {
            name: string;
            version: string;
            /** @description Location gathered from, `oci::host/path`. */
            repository: string;
            /** Format: date-time */
            checkedAt: string;
            levels: components["schemas"]["AttestationLevel"][];
        };
        /** @description An OCI image and the platforms it offers. */
        ImageIndex: {
            repository: string;
            digest: string;
            mediaType: string;
            platforms: components["schemas"]["ImagePlatform"][];
        };
        ImagePlatform: {
            /** @description `os/arch[/variant]`; empty for a single manifest whose config lacks os or arch. */
            platform: string;
            /** @description Digest of the platform manifest. */
            digest: string;
        };
        /** @description One platform of an image, from its manifest and config. */
        ImageDetails: {
            digest: string;
            platform: string;
            /**
             * Format: int64
             * @description Config plus layers, compressed bytes.
             */
            size: number;
            created?: string;
            user?: string;
            workingDir?: string;
            entrypoint?: string[];
            cmd?: string[];
            exposedPorts?: string[];
            env?: string[];
            labels?: {
                [key: string]: string;
            };
            layers: components["schemas"]["ImageLayer"][];
        };
        ImageLayer: {
            digest: string;
            /** Format: int64 */
            size: number;
            createdBy?: string;
        };
        /** @description A parsed document; at least one of `sbom`, `vex` and `scan` is set. A CycloneDX document may carry all three: its analysed vulnerabilities are `vex` statements (states mapped to OpenVEX statuses), the others `scan` findings. Nothing is verified. */
        Document: {
            /** @description e.g. `CycloneDX 1.6`, `OpenVEX`, `SARIF 2.1.0`. */
            format: string;
            sbom?: components["schemas"]["DocumentSBOM"];
            vex?: components["schemas"]["DocumentVEX"];
            scan?: components["schemas"]["DocumentScan"];
        };
        DocumentSBOM: {
            packages: components["schemas"]["SBOMPackage"][];
        };
        SBOMPackage: {
            name: string;
            version?: string;
            license?: string;
            /** @description Package URL type, e.g. `npm`. */
            type?: string;
        };
        DocumentVEX: {
            statements: components["schemas"]["VEXStatement"][];
        };
        VEXStatement: {
            vulnerability: string;
            products: string[];
            status: string;
            justification?: string;
            action?: string;
        };
        DocumentScan: {
            tool: string;
            /** @description `security-severity` when any finding has a score, else `level`. */
            importantBasis: string;
            findings: components["schemas"]["ScanFinding"][];
        };
        ScanFinding: {
            rule: string;
            severity?: string;
            level?: string;
            location?: string;
            message: string;
        };
        ErrorItem: {
            code: components["schemas"]["ErrorCode"];
            message: string;
            detail: {
                [key: string]: unknown;
            };
        };
        FacetValue: {
            value: string;
            count: number;
        };
        ConnectorFacet: {
            /** @description Source key, e.g. `github:org:open-component-model`. */
            key: string;
            title: string;
            /** @description Connector type, e.g. `github/v1`. */
            type: string;
            /** @description Owner avatar; absent if unknown. */
            avatarUrl?: string;
            count: number;
            /** @description Owners below this source (e.g. an enterprise's orgs), by count. */
            groups: components["schemas"]["GroupFacet"][];
        };
        GroupFacet: {
            /** @description Owner key, e.g. `github:org:open-component-model`. */
            key: string;
            title: string;
            avatarUrl?: string;
            count: number;
        };
        SourceGroup: {
            key: string;
            title: string;
            avatarUrl?: string;
            locations: string[];
        };
        Facets: {
            /** @description Sources in hub.yaml order; sources without components are left out. */
            connectors: components["schemas"]["ConnectorFacet"][];
            providers: components["schemas"]["FacetValue"][];
            categories: components["schemas"]["FacetValue"][];
            /** @description Platforms of the default versions (descriptor and image platforms read by the sync). */
            architectures: components["schemas"]["FacetValue"][];
            /** @description `single` and `replicated` (see the `replication` filter); values without components are left out. */
            replication: components["schemas"]["FacetValue"][];
        };
        ComponentSummary: {
            name: string;
            /** @description Newest stable version by publish date; absent if none. */
            latest?: string;
            /** Format: date-time */
            latestPublishedAt?: string;
            /** @description Label `catalog.chrisschneider.dev/title`; falls back to the name. */
            title: string;
            description?: string;
            /** @description An https or data URL, or `lucide/<name>`. */
            icon?: string;
            categories?: string[];
            provider?: string;
            versionCount: number;
            locations: string[];
            /** @description The default version carries at least one signature (not verified). */
            signed: boolean;
            /** @description The version shown by default: `latest`, or the newest pre-release when there is no stable version. */
            defaultVersion: string;
            /** @description Avatar of the GitHub org hosting the component (never a personal account); the UI shows it when there is no icon. */
            ownerAvatarUrl?: string;
        };
        ComponentList: {
            components: components["schemas"]["ComponentSummary"][];
            /** @description Cursor of the next page; absent on the last page. */
            next?: string;
            /** @description Components matching the filters over all pages. */
            total: number;
            facets: components["schemas"]["Facets"];
        };
        LocationSummary: {
            repository: string;
            latest?: string;
            versionCount: number;
        };
        Source: {
            key: string;
            title: string;
            type: string;
            avatarUrl?: string;
            /** @description Where this source found the component. */
            locations: string[];
            groups: components["schemas"]["SourceGroup"][];
        };
        Component: {
            name: string;
            latest?: string;
            /** Format: date-time */
            latestPublishedAt?: string;
            title: string;
            description?: string;
            icon?: string;
            categories?: string[];
            provider?: string;
            versionCount: number;
            locations: components["schemas"]["LocationSummary"][];
            /** @description Sources that found the component */
            sources: components["schemas"]["Source"][];
            links: components["schemas"]["ComponentLinks"];
            signed: boolean;
            /** @description The version shown by default: `latest`, or the newest pre-release when there is no stable version. */
            defaultVersion: string;
            /** @description Parents: components whose latest version references this one, by name. */
            referencedBy: components["schemas"]["ParentRef"][];
            referencedByCount: number;
            /** @description Avatar of the GitHub org hosting the component (never a personal account); the UI shows it when there is no icon. */
            ownerAvatarUrl?: string;
        };
        ParentRef: {
            name: string;
            title: string;
            latest?: string;
        };
        ComponentVersionOverview: {
            name: string;
            version: string;
            /** @description Location the descriptor was read from. */
            repository: string;
            /** @description Label `catalog.chrisschneider.dev/title`; falls back to the name. */
            title: string;
            description?: string;
            icon?: string;
            categories?: string[];
            provider?: string;
            /** @description At least one signature (not verified). */
            signed: boolean;
            /**
             * Format: date-time
             * @description Earliest publish time of this version across indexed locations; absent when the index does not know it.
             */
            publishedAt?: string;
            kubernetes?: components["schemas"]["KubernetesCompatibility"];
            /** @description Volumes it needs (label `catalog.chrisschneider.dev/compatibility`), in label order; absent when none are declared. */
            storage?: components["schemas"]["StorageNeed"][];
            responsibles: components["schemas"]["Responsible"][];
            riskProfile?: components["schemas"]["RiskProfile"];
            docs: components["schemas"]["DocLink"][];
            contents: components["schemas"]["ContentsSummary"];
            /** @description Platforms from the resources' extra identity (`os`/`architecture`) and from the image platforms read live, sorted by platform. */
            architectures: components["schemas"]["Architecture"][];
            /** @description Every OCI image's platforms were read; false when a read failed or timed out, or no registry is configured. */
            imagePlatformsRead: boolean;
            /** @description The component version's resources as in the OCM v2 descriptor (descriptor order). */
            resources: components["schemas"]["Resource"][];
            /** @description The component version's component references as in the OCM v2 descriptor (descriptor order). */
            references: components["schemas"]["ComponentReference"][];
            /** @description The component version's sources as in the OCM v2 descriptor (e.g. the git repository and commit it was built from). */
            sources: components["schemas"]["ComponentSource"][];
            /** @description The descriptor's signatures as in the OCM v2 descriptor (not verified). */
            signatures: components["schemas"]["Signature"][];
        };
        Architecture: {
            /** @description `os/architecture[/variant]`, or the architecture alone. */
            platform: string;
            /** @description Where the platform was seen, sorted. */
            from: ("ocm" | "image")[];
        };
        KubernetesCompatibility: {
            /** @description Semver constraint from the label. */
            constraint: string;
            /** @description Known Kubernetes minors the constraint admits. */
            versions: string[];
            distributions: string[];
        };
        StorageNeed: {
            /**
             * @description Kubernetes access mode (ReadWriteOnce, ReadWriteMany, ReadOnlyMany, ReadWriteOncePod).
             * @enum {string}
             */
            accessMode: "RWO" | "RWX" | "ROX" | "RWOP";
            /** @enum {string} */
            volumeMode: "Filesystem" | "Block";
            purpose?: string;
        };
        Responsible: {
            /** @enum {string} */
            type: "githubUser" | "githubTeam" | "codeowners";
            username?: string;
            /** @description org/team */
            teamname?: string;
            githubHostname?: string;
        };
        RiskProfile: {
            /** @enum {string} */
            networkExposure?: "private" | "protected" | "public";
            authenticationEnforced?: boolean;
            /** @enum {string} */
            userInteraction?: "operator" | "end-user";
            /** @enum {string} */
            confidentialityRequirement?: "none" | "low" | "high";
            /** @enum {string} */
            integrityRequirement?: "none" | "low" | "high";
            /** @enum {string} */
            availabilityRequirement?: "none" | "low" | "high";
            comment?: string;
        };
        DocLink: {
            title: string;
            /** Format: uri */
            url: string;
            /** @enum {string} */
            kind?: "docs" | "api" | "tutorial" | "issues" | "website";
        };
        ContentsSummary: {
            resources: components["schemas"]["TypeCount"][];
            references: number;
        };
        TypeCount: {
            type: string;
            count: number;
        };
        /** @description OCM v2 resource (ocm-spec, component descriptor `resources[]`). */
        Resource: {
            name: string;
            version: string;
            extraIdentity?: {
                [key: string]: string;
            };
            labels?: {
                [key: string]: unknown;
            }[];
            srcRefs?: {
                [key: string]: unknown;
            }[];
            type: string;
            /** @enum {string} */
            relation: "local" | "external";
            access: {
                [key: string]: unknown;
            };
            digest?: components["schemas"]["Digest"];
        };
        /** @description OCM v2 source (ocm-spec, component descriptor `sources[]`). */
        ComponentSource: {
            name: string;
            version: string;
            extraIdentity?: {
                [key: string]: string;
            };
            labels?: {
                [key: string]: unknown;
            }[];
            type: string;
            access: {
                [key: string]: unknown;
            };
        };
        /** @description OCM v2 component reference (ocm-spec, component descriptor `componentReferences[]`). */
        ComponentReference: {
            name: string;
            version: string;
            extraIdentity?: {
                [key: string]: string;
            };
            labels?: {
                [key: string]: unknown;
            }[];
            componentName: string;
            digest?: components["schemas"]["Digest"];
        };
        /** @description OCM v2 signature (ocm-spec, component descriptor `signatures[]`). */
        Signature: {
            name: string;
            digest: components["schemas"]["Digest"];
            signature: {
                algorithm: string;
                value: string;
                mediaType: string;
                issuer?: string;
            };
        };
        Digest: {
            hashAlgorithm: string;
            normalisationAlgorithm: string;
            value: string;
        };
        ComponentLinks: {
            versions: string;
            latest?: string;
        };
        VersionEntry: {
            version: string;
            /** Format: date-time */
            publishedAt: string;
            stable: boolean;
            locations: string[];
            /** @description Whether the version has a changelog resource; absent: unknown. */
            hasChangelog?: boolean;
        };
        VersionList: {
            name: string;
            versions: components["schemas"]["VersionEntry"][];
            next?: string;
        };
        LocatedDescriptor: {
            /** @description Where the descriptor was read; differs from the root's when a resolver found it elsewhere. */
            location: string;
            descriptor: components["schemas"]["Descriptor"];
        };
        /** @description A descriptor, or with `recursive=-1` a list of located descriptors. */
        DescriptorOrLocatedList: components["schemas"]["Descriptor"] | components["schemas"]["LocatedDescriptor"][];
        Selector: {
            matchIdentity?: {
                [key: string]: string;
            };
            matchLabels?: {
                [key: string]: string;
            };
            /** @description CEL; reserved */
            expression?: string;
        };
        /** @description Exactly one mode. */
        Extract: {
            byComponents?: {
                [key: string]: string;
            };
            byResources?: {
                [key: string]: string;
            };
            expression?: string;
        };
        DiscoveryRequest: {
            componentSelector?: components["schemas"]["Selector"];
            resourceSelector?: components["schemas"]["Selector"];
            extract?: components["schemas"]["Extract"];
        };
        /** @description Exactly one of components or extracted. */
        DiscoveryResult: {
            components?: components["schemas"]["Descriptor"][];
            extracted?: {
                [key: string]: unknown;
            }[];
        };
        /** @description OCM component descriptor, schema version v2 (see ocm.software). */
        Descriptor: {
            [key: string]: unknown;
        };
    };
    responses: {
        /** @description The representation matches If-None-Match (no body). */
        NotModified: {
            headers: {
                ETag: components["headers"]["ETag"];
                "Cache-Control": components["headers"]["CacheControl"];
                Vary?: string;
                [name: string]: unknown;
            };
            content?: never;
        };
        /** @description Error in the OCI error envelope. */
        Error: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorResponse"];
            };
        };
        /** @description `latest` resolved to a concrete version. */
        Redirect: {
            headers: {
                Location?: string;
                "Cache-Control": components["headers"]["CacheControl"];
                [name: string]: unknown;
            };
            content?: never;
        };
    };
    parameters: {
        /** @description OCM component name; contains `/`, send percent-encoded. */
        Name: string;
        /** @description OCM version, or `latest`. */
        Version: string;
        /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
        Repository: string;
        /** @description Digest of an attestation entry, as listed in its set (e.g. `sha256:<hex>`). */
        AttestationDigest: string;
        /** @description Resource name; add `extraIdentity.<key>=<value>` query parameters to pick one of several with the same name. */
        ImageResource: string;
        /** @description Digest of a platform manifest, as listed by the platforms route (e.g. `sha256:<hex>`). */
        ImageDigest: string;
        /** @description `manifest` or `config`: send the stored JSON instead of the summary. */
        Raw: "manifest" | "config";
        /** @description `document`: answer the parsed `Document` (SBOM, VEX and/or scan results) instead of the bytes. */
        As: "document";
        /** @description Like the ocm CLI's `--recursive`. `0` (default): this descriptor, as an object. `-1`: an array with this descriptor and, recursively, every referenced component version, each with the location it was read from: depth-first in `componentReferences` order (the CLI's order), each `name:version` once. Values above 0 are not implemented (400). */
        Recursive: number;
        /** @description `true`: trim each returned descriptor to what a component tree needs — `meta` and `component.name`, `version`, `provider`, `labels`, `componentReferences`; `resources`, `sources` and `repositoryContexts` are null, signatures omitted. The result is not a complete OCM descriptor. Combine with `recursive=-1` to load a whole tree cheaply. */
        Compact: boolean;
        IfNoneMatch: string;
        ListLimit: number;
        VersionsLimit: number;
        /** @description Opaque; the `next` of the previous page. */
        Cursor: string;
        /** @description Include pre-releases (alpha, beta, rc, … — see versions.prereleaseMarkers). */
        IncludePrerelease: boolean;
        /** @description Repeatable; a location such as `oci::ghcr.io/open-component-model`. */
        LocationFilter: string[];
        /** @description Repeatable; a source key (`github:enterprise:neonephos`) or an owner key (`github:org:open-component-model`). Several match any. */
        ConnectorFilter: string[];
    };
    requestBodies: never;
    headers: {
        /** @description Validator; send it back in If-None-Match to get 304. Index routes send a weak ETag (`W/"<index generation>-<hash of the request>"`). */
        ETag: string;
        /** @description `no-cache` on successful responses (revalidate with the ETag), `no-store` on errors. */
        CacheControl: string;
    };
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    getHealth: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Server is alive. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "text/plain": string;
                };
            };
        };
    };
    getBase: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description API version check (like OCI's `GET /v2/`). */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": Record<string, never>;
                };
            };
        };
    };
    listComponents: {
        parameters: {
            query?: {
                /** @description Case-insensitive substring of name or title. */
                q?: string;
                /** @description Repeatable; a location such as `oci::ghcr.io/open-component-model`. */
                location?: components["parameters"]["LocationFilter"];
                /** @description Repeatable; a source key (`github:enterprise:neonephos`) or an owner key (`github:org:open-component-model`). Several match any. */
                connector?: components["parameters"]["ConnectorFilter"];
                provider?: string;
                /** @description Repeatable; all must match. */
                category?: string[];
                /** @description Platform such as `linux/arm64`. Repeatable; all must match. */
                architecture?: string[];
                /** @description Include pre-releases (alpha, beta, rc, … — see versions.prereleaseMarkers). */
                includePrerelease?: components["parameters"]["IncludePrerelease"];
                /** @description RFC 3339; latest published at or after. */
                updatedSince?: string;
                /** @description Only components without a parent of the same family (same provider and name prefix). */
                topLevel?: boolean;
                /** @description `single`: components in one location; `replicated`: in two or more. */
                replication?: "single" | "replicated";
                /** @description `relevance`: components included by many others and top-level components bundling many first, then most recently updated. */
                sort?: "relevance" | "updated" | "name";
                limit?: components["parameters"]["ListLimit"];
                /** @description Opaque; the `next` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description A page of components with facets. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    "Cache-Control": components["headers"]["CacheControl"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ComponentList"];
                };
            };
            304: components["responses"]["NotModified"];
            default: components["responses"]["Error"];
        };
    };
    headComponents: {
        parameters: {
            query?: {
                q?: string;
                /** @description Repeatable; a location such as `oci::ghcr.io/open-component-model`. */
                location?: components["parameters"]["LocationFilter"];
                provider?: string;
                category?: string[];
                architecture?: string[];
                /** @description Include pre-releases (alpha, beta, rc, … — see versions.prereleaseMarkers). */
                includePrerelease?: components["parameters"]["IncludePrerelease"];
                updatedSince?: string;
                /** @description Only components without a parent of the same family (same provider and name prefix). */
                topLevel?: boolean;
                /** @description `single`: components in one location; `replicated`: in two or more. */
                replication?: "single" | "replicated";
                /** @description `relevance`: components included by many others and top-level components bundling many first, then most recently updated. */
                sort?: "relevance" | "updated" | "name";
                limit?: components["parameters"]["ListLimit"];
                /** @description Opaque; the `next` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Same headers as GET, no body. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    "Cache-Control": components["headers"]["CacheControl"];
                    [name: string]: unknown;
                };
                content?: never;
            };
            304: components["responses"]["NotModified"];
            /** @description Error (no body on HEAD). */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
        };
    };
    getComponent: {
        parameters: {
            query?: never;
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Component overview. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    "Cache-Control": components["headers"]["CacheControl"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Component"];
                };
            };
            304: components["responses"]["NotModified"];
            default: components["responses"]["Error"];
        };
    };
    headComponent: {
        parameters: {
            query?: never;
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Same headers as GET, no body. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    "Cache-Control": components["headers"]["CacheControl"];
                    [name: string]: unknown;
                };
                content?: never;
            };
            304: components["responses"]["NotModified"];
            /** @description Error (no body on HEAD). */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
        };
    };
    listVersions: {
        parameters: {
            query?: {
                /** @description Include pre-releases (alpha, beta, rc, … — see versions.prereleaseMarkers). */
                includePrerelease?: components["parameters"]["IncludePrerelease"];
                /** @description Masterminds syntax, e.g. `>=1.0.0, <2.0.0`; drops non-semver versions; filter only. */
                semverConstraint?: string;
                /** @description Repeatable; a location such as `oci::ghcr.io/open-component-model`. */
                location?: components["parameters"]["LocationFilter"];
                limit?: components["parameters"]["VersionsLimit"];
                /** @description Opaque; the `next` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Versions, newest published first. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    "Cache-Control": components["headers"]["CacheControl"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["VersionList"];
                };
            };
            304: components["responses"]["NotModified"];
            default: components["responses"]["Error"];
        };
    };
    headVersions: {
        parameters: {
            query?: {
                /** @description Include pre-releases (alpha, beta, rc, … — see versions.prereleaseMarkers). */
                includePrerelease?: components["parameters"]["IncludePrerelease"];
                semverConstraint?: string;
                /** @description Repeatable; a location such as `oci::ghcr.io/open-component-model`. */
                location?: components["parameters"]["LocationFilter"];
                limit?: components["parameters"]["VersionsLimit"];
                /** @description Opaque; the `next` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Same headers as GET, no body. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    "Cache-Control": components["headers"]["CacheControl"];
                    [name: string]: unknown;
                };
                content?: never;
            };
            304: components["responses"]["NotModified"];
            /** @description Error (no body on HEAD). */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
        };
    };
    getDescriptor: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
                /** @description Like the ocm CLI's `--recursive`. `0` (default): this descriptor, as an object. `-1`: an array with this descriptor and, recursively, every referenced component version, each with the location it was read from: depth-first in `componentReferences` order (the CLI's order), each `name:version` once. Values above 0 are not implemented (400). */
                recursive?: components["parameters"]["Recursive"];
                /** @description `true`: trim each returned descriptor to what a component tree needs — `meta` and `component.name`, `version`, `provider`, `labels`, `componentReferences`; `resources`, `sources` and `repositoryContexts` are null, signatures omitted. The result is not a complete OCM descriptor. Combine with `recursive=-1` to load a whole tree cheaply. */
                compact?: components["parameters"]["Compact"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Component descriptor (YAML by default, JSON via Accept); with recursive=-1 a list of located descriptors. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Strong ETag, the sha256 of the response body. */
                    ETag?: string;
                    /** @description Always `Accept` (the encoding depends on it). */
                    Vary?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/yaml": components["schemas"]["DescriptorOrLocatedList"];
                    "application/json": components["schemas"]["DescriptorOrLocatedList"];
                };
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            default: components["responses"]["Error"];
        };
    };
    headDescriptor: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
                /** @description Like the ocm CLI's `--recursive`. `0` (default): this descriptor, as an object. `-1`: an array with this descriptor and, recursively, every referenced component version, each with the location it was read from: depth-first in `componentReferences` order (the CLI's order), each `name:version` once. Values above 0 are not implemented (400). */
                recursive?: components["parameters"]["Recursive"];
                /** @description `true`: trim each returned descriptor to what a component tree needs — `meta` and `component.name`, `version`, `provider`, `labels`, `componentReferences`; `resources`, `sources` and `repositoryContexts` are null, signatures omitted. The result is not a complete OCM descriptor. Combine with `recursive=-1` to load a whole tree cheaply. */
                compact?: components["parameters"]["Compact"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Descriptor exists (same headers as GET, no body). */
            200: {
                headers: {
                    ETag?: string;
                    "Cache-Control": components["headers"]["CacheControl"];
                    Vary?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            /** @description Error (no body on HEAD). */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
        };
    };
    getVersionOverview: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The overview. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Strong ETag, the sha256 of the response body. */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ComponentVersionOverview"];
                };
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            default: components["responses"]["Error"];
        };
    };
    headVersionOverview: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Same headers as GET, no body. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Strong ETag. */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            /** @description Error (no body on HEAD). */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
        };
    };
    getAttestations: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The attestation set. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Weak ETag of the index generation and the request. */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AttestationSet"];
                };
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            default: components["responses"]["Error"];
        };
    };
    headAttestations: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Same headers as GET, no body. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Weak ETag. */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            /** @description Error (no body on HEAD). */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
        };
    };
    getAttestationContent: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
                /** @description `document`: answer the parsed `Document` (SBOM, VEX and/or scan results) instead of the bytes. */
                as?: components["parameters"]["As"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
                /** @description Digest of an attestation entry, as listed in its set (e.g. `sha256:<hex>`). */
                digest: components["parameters"]["AttestationDigest"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The stored document, or with `as=document` the parsed `Document`. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Strong ETag, the sha256 of the response body. */
                    ETag?: string;
                    /** @description Without `as`: `attachment; filename="<first 12 hex of the digest>.json"`. */
                    "Content-Disposition"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Document"] | unknown;
                };
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            default: components["responses"]["Error"];
        };
    };
    headAttestationContent: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
                /** @description `document`: answer the parsed `Document` (SBOM, VEX and/or scan results) instead of the bytes. */
                as?: components["parameters"]["As"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
                /** @description Digest of an attestation entry, as listed in its set (e.g. `sha256:<hex>`). */
                digest: components["parameters"]["AttestationDigest"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Same headers as GET, no body. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Strong ETag. */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            /** @description Error (no body on HEAD). */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
        };
    };
    getResource: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
                /** @description `document`: answer the parsed `Document` (SBOM, VEX and/or scan results) instead of the bytes. */
                as?: components["parameters"]["As"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
                resource: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Resource content, or with `as=document` the parsed `Document`. */
            200: {
                headers: {
                    /**
                     * @description The resource digest from the OCM component descriptor (e.g. `sha256:<hex>`),
                     *     when the descriptor carries one. For non-local access types (ociImage, helm,
                     *     wget, …) this is OCM's digest of the artifact (possibly normalised), not a
                     *     hash of the bytes served here.
                     */
                    "OCM-Resource-Digest"?: string;
                    /**
                     * @description Strong ETag; sent only when the served bytes are exactly the digested
                     *     artifact (localBlob resources), then equal to `OCM-Resource-Digest`.
                     */
                    ETag?: string;
                    "Content-Disposition"?: string;
                    "Cache-Control": components["headers"]["CacheControl"];
                    [name: string]: unknown;
                };
                content: {
                    "*/*": unknown;
                };
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            default: components["responses"]["Error"];
        };
    };
    headResource: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
                /** @description `document`: answer the parsed `Document` (SBOM, VEX and/or scan results) instead of the bytes. */
                as?: components["parameters"]["As"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
                resource: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Resource exists. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            /** @description Error (no body on HEAD). */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
        };
    };
    getImageIndex: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
                /** @description Resource name; add `extraIdentity.<key>=<value>` query parameters to pick one of several with the same name. */
                resource: components["parameters"]["ImageResource"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The image and its platforms. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Strong ETag. */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ImageIndex"];
                };
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            default: components["responses"]["Error"];
        };
    };
    headImageIndex: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
                /** @description Resource name; add `extraIdentity.<key>=<value>` query parameters to pick one of several with the same name. */
                resource: components["parameters"]["ImageResource"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Same headers as GET, no body. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Strong ETag. */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            /** @description Error (no body on HEAD). */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
        };
    };
    getImageDetails: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
                /** @description `manifest` or `config`: send the stored JSON instead of the summary. */
                raw?: components["parameters"]["Raw"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
                /** @description Resource name; add `extraIdentity.<key>=<value>` query parameters to pick one of several with the same name. */
                resource: components["parameters"]["ImageResource"];
                /** @description Digest of a platform manifest, as listed by the platforms route (e.g. `sha256:<hex>`). */
                digest: components["parameters"]["ImageDigest"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The platform details, or with `raw` the manifest or config JSON. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Strong ETag. */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ImageDetails"];
                    "application/vnd.oci.image.manifest.v1+json": Record<string, never>;
                    "application/vnd.oci.image.config.v1+json": Record<string, never>;
                };
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            default: components["responses"]["Error"];
        };
    };
    headImageDetails: {
        parameters: {
            query?: {
                /** @description Pin a location (ocm CLI syntax, e.g. `oci::ghcr.io/acme`): one known to the index or a repository of a configured resolver. */
                repository?: components["parameters"]["Repository"];
                /** @description `manifest` or `config`: send the stored JSON instead of the summary. */
                raw?: components["parameters"]["Raw"];
            };
            header?: {
                "If-None-Match"?: components["parameters"]["IfNoneMatch"];
            };
            path: {
                /** @description OCM component name; contains `/`, send percent-encoded. */
                name: components["parameters"]["Name"];
                /** @description OCM version, or `latest`. */
                version: components["parameters"]["Version"];
                /** @description Resource name; add `extraIdentity.<key>=<value>` query parameters to pick one of several with the same name. */
                resource: components["parameters"]["ImageResource"];
                /** @description Digest of a platform manifest, as listed by the platforms route (e.g. `sha256:<hex>`). */
                digest: components["parameters"]["ImageDigest"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Same headers as GET, no body. */
            200: {
                headers: {
                    "Cache-Control": components["headers"]["CacheControl"];
                    /** @description Strong ETag. */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content?: never;
            };
            304: components["responses"]["NotModified"];
            307: components["responses"]["Redirect"];
            /** @description Error (no body on HEAD). */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
        };
    };
    discover: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["DiscoveryRequest"];
            };
        };
        responses: {
            /** @description Discovery result. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DiscoveryResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
