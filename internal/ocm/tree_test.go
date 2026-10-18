package ocm

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
)

func ids(located []Located) []string {
	var out []string
	for _, l := range located {
		out = append(out, l.Descriptor.Component.Name+":"+l.Descriptor.Component.Version)
	}
	return out
}

// everywhere is a HasVersionAt that reports every version at the root's
// location, so the whole tree is read there.
func everywhere(context.Context, string, string, string) (bool, error) { return true, nil }

func TestDescriptorTreeAt(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host, SubPath: "tree"}
	push := func(name, version string, refs ...fixtures.Reference) {
		fixtures.Push(ctx, t, target, fixtures.Component{Name: name, Version: version, References: refs})
	}
	// The how-to example: product → backend, frontend, cache; backend → cache.
	push("acme.org/how-to/cache", "2.0.0")
	push("acme.org/how-to/frontend", "1.5.0")
	push("acme.org/how-to/backend", "3.1.0", fixtures.Reference{Name: "cache", Component: "acme.org/how-to/cache", Version: "2.0.0"})
	push("acme.org/how-to/product", "2.0.0",
		fixtures.Reference{Name: "backend", Component: "acme.org/how-to/backend", Version: "3.1.0"},
		fixtures.Reference{Name: "frontend", Component: "acme.org/how-to/frontend", Version: "1.5.0"},
		fixtures.Reference{Name: "cache", Component: "acme.org/how-to/cache", Version: "2.0.0"})
	// Two versions of one component, a cycle, and a dangling reference.
	push("acme.org/how-to/cache", "1.9.0")
	push("acme.org/two", "1.0.0",
		fixtures.Reference{Name: "new", Component: "acme.org/how-to/cache", Version: "2.0.0"},
		fixtures.Reference{Name: "old", Component: "acme.org/how-to/cache", Version: "1.9.0"})
	push("acme.org/cycle-a", "1.0.0", fixtures.Reference{Name: "b", Component: "acme.org/cycle-b", Version: "1.0.0"})
	push("acme.org/cycle-b", "1.0.0", fixtures.Reference{Name: "a", Component: "acme.org/cycle-a", Version: "1.0.0"})
	push("acme.org/dangling", "1.0.0", fixtures.Reference{Name: "gone", Component: "acme.org/gone", Version: "9.9.9"})

	svc := newTestService(t, []fixtures.Resolver{{Pattern: "acme.org/*", Host: reg.Host, SubPath: "tree"}}, nil)
	loc, err := LocationKey("oci::http://" + reg.Host + "/tree")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, component, version string
		want                     []string
	}{
		{"how-to: CLI order, duplicates removed", "acme.org/how-to/product", "2.0.0",
			[]string{"acme.org/how-to/product:2.0.0", "acme.org/how-to/backend:3.1.0", "acme.org/how-to/cache:2.0.0", "acme.org/how-to/frontend:1.5.0"}},
		{"leaf", "acme.org/how-to/frontend", "1.5.0", []string{"acme.org/how-to/frontend:1.5.0"}},
		{"two versions of one component", "acme.org/two", "1.0.0",
			[]string{"acme.org/two:1.0.0", "acme.org/how-to/cache:2.0.0", "acme.org/how-to/cache:1.9.0"}},
		{"cycle terminates", "acme.org/cycle-a", "1.0.0", []string{"acme.org/cycle-a:1.0.0", "acme.org/cycle-b:1.0.0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			got, err := svc.DescriptorTreeAt(ctx, Ref{Component: tc.component, Version: tc.version, Repository: loc}, everywhere)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids(got), tc.want) {
				t.Fatalf("got %v, want %v", ids(got), tc.want)
			}
			if time.Since(start) > 10*time.Second {
				t.Fatalf("took %s (deadlock until timeout?)", time.Since(start))
			}
		})
	}
	t.Run("dangling reference fails and names it", func(t *testing.T) {
		_, err := svc.DescriptorTreeAt(ctx, Ref{Component: "acme.org/dangling", Version: "1.0.0", Repository: loc}, everywhere)
		if !errors.Is(err, ErrComponentUnknown) || !strings.Contains(err.Error(), `reference "gone"`) || !strings.Contains(err.Error(), "acme.org/gone:9.9.9") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unknown root version", func(t *testing.T) {
		if _, err := svc.DescriptorTreeAt(ctx, Ref{Component: "acme.org/how-to/product", Version: "9.9.9", Repository: loc}, everywhere); !errors.Is(err, ErrVersionUnknown) {
			t.Fatalf("err = %v", err)
		}
	})
}
