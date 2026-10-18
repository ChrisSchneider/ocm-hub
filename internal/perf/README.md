# Performance tests

Index query latency over a synthetic index (2000 components x 100 versions x 2 locations); no Docker needed:

    go test -tags perf -run TestPerfIndexQueries -v -timeout 30m ./internal/perf/

Each endpoint (list, search+facets, by name, overview, versions) is attacked at 100 req/s for 10 s
and fails if any request errors or p95 exceeds 50 ms.
