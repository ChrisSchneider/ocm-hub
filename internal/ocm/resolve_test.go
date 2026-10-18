package ocm

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
	ocirepositoryv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

func TestNewResolverRejectsUnsupported(t *testing.T) {
	cfgCTF := writeRaw(t, `type: generic.config.ocm.software/v1
configurations:
- type: resolvers.config.ocm.software/v1alpha1
  resolvers:
  - repository:
      type: CommonTransportFormat/v1
      filePath: /tmp/archive
`)
	b := newTestBindings(t, cfgCTF)
	if _, err := NewResolver(t.Context(), b); err == nil || !strings.Contains(err.Error(), "not supported yet") {
		t.Fatalf("err = %v, want 'not supported yet'", err)
	}
}

func TestResolverSemantics(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	a := fixtures.Target{Host: reg.Host, SubPath: "resolve-a"}
	bTarget := fixtures.Target{Host: reg.Host, SubPath: "resolve-b"}
	data := []fixtures.Resource{{Name: "d", Type: "plainText", MediaType: "text/plain", Content: []byte("x")}}
	fixtures.Push(ctx, t, a, fixtures.Component{Name: "acme.org/routed", Version: "1.0.0", Resources: data})
	fixtures.Push(ctx, t, bTarget, fixtures.Component{Name: "acme.org/routed", Version: "2.0.0", Resources: data})
	fixtures.Push(ctx, t, bTarget, fixtures.Component{Name: "acme.org/only-b", Version: "1.0.0", Resources: data})

	b := newTestBindings(t, fixtures.WriteOCMConfig(t, []fixtures.Resolver{
		{Pattern: "acme.org/routed", Constraint: ">=2.0.0", Host: reg.Host, SubPath: "resolve-b"},
		{Pattern: "acme.org/*", Host: reg.Host, SubPath: "resolve-a"},
	}, nil))
	r, err := NewResolver(ctx, b)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	keyA := "http://" + strings.ToLower(reg.Host) + "/resolve-a"
	keyB := "http://" + strings.ToLower(reg.Host) + "/resolve-b"

	t.Run("version constraint routes 2.x to b", func(t *testing.T) {
		_, key, err := r.Repository(ctx, "acme.org/routed", "2.0.0", "")
		if err != nil || key != keyB {
			t.Fatalf("key=%q err=%v, want %q", key, err, keyB)
		}
	})
	t.Run("1.x goes to the unconstrained resolver", func(t *testing.T) {
		_, key, err := r.Repository(ctx, "acme.org/routed", "1.0.0", "")
		if err != nil || key != keyA {
			t.Fatalf("key=%q err=%v, want %q", key, err, keyA)
		}
	})
	t.Run("no version skips constrained resolvers", func(t *testing.T) {
		repo, key, err := r.Repository(ctx, "acme.org/routed", "", "")
		if err != nil || key != keyA {
			t.Fatalf("key=%q err=%v, want %q", key, err, keyA)
		}
		versions, err := repo.ListComponentVersions(ctx, "acme.org/routed")
		if err != nil || !slices.Equal(versions, []string{"1.0.0"}) {
			t.Fatalf("versions=%v err=%v, want [1.0.0]", versions, err)
		}
	})
	t.Run("Resolve returns spec, key and repository", func(t *testing.T) {
		repo, err := r.Resolve(ctx, "acme.org/only-b", "1.0.0", "")
		if err != nil || repo.CVR == nil || repo.Spec == nil || repositoryKey(repo.Spec) != repo.Key {
			t.Fatalf("repo = %+v, err = %v", repo, err)
		}
	})
	t.Run("first match wins, no fallback", func(t *testing.T) {
		repo, key, err := r.Repository(ctx, "acme.org/only-b", "1.0.0", "")
		if err != nil || key != keyA {
			t.Fatalf("key=%q err=%v, want %q", key, err, keyA)
		}
		if _, err := repo.GetComponentVersion(ctx, "acme.org/only-b", "1.0.0"); err == nil {
			t.Fatal("expected not found in repository a")
		}
	})
	t.Run("no matching resolver", func(t *testing.T) {
		if _, _, err := r.Repository(ctx, "other.org/x", "1.0.0", ""); !errors.Is(err, ErrComponentUnknown) {
			t.Fatalf("err = %v, want ErrComponentUnknown", err)
		}
	})
	t.Run("pinned configured repository is used exclusively", func(t *testing.T) {
		repo, key, err := r.Repository(ctx, "acme.org/only-b", "1.0.0", "oci::http://"+reg.Host+"/resolve-b")
		if err != nil || key != keyB {
			t.Fatalf("key=%q err=%v, want %q", key, err, keyB)
		}
		if _, err := repo.GetComponentVersion(ctx, "acme.org/only-b", "1.0.0"); err != nil {
			t.Fatalf("GetComponentVersion via pinned repo: %v", err)
		}
	})
	t.Run("pinned unknown repository is rejected", func(t *testing.T) {
		if _, _, err := r.Repository(ctx, "acme.org/only-b", "1.0.0", "oci::http://evil.example.com/x"); !errors.Is(err, ErrRepositoryNotAllowed) {
			t.Fatalf("err = %v, want ErrRepositoryNotAllowed", err)
		}
	})
	t.Run("pinned garbage is invalid", func(t *testing.T) {
		if _, _, err := r.Repository(ctx, "acme.org/only-b", "1.0.0", "ctf::"); !errors.Is(err, ErrRepositoryInvalid) {
			t.Fatalf("err = %v, want ErrRepositoryInvalid", err)
		}
	})
}

func writeRaw(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/.ocmconfig"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRepositoryKey(t *testing.T) {
	key := func(base, sub string) string {
		return repositoryKey(&ocirepositoryv1.Repository{BaseUrl: base, SubPath: sub})
	}
	tests := []struct {
		name       string
		a, b       [2]string // {baseUrl, subPath}
		equal      bool
		wantKeyOfA string
	}{
		{"uppercase host", [2]string{"GHCR.IO", "acme"}, [2]string{"ghcr.io", "acme"}, true, "ghcr.io/acme"},
		{"trailing slash", [2]string{"ghcr.io/", "/acme/"}, [2]string{"ghcr.io", "acme"}, true, "ghcr.io/acme"},
		{"https prefix stripped", [2]string{"https://ghcr.io", "acme"}, [2]string{"ghcr.io", "acme"}, true, "ghcr.io/acme"},
		{"HTTPS prefix stripped", [2]string{"HTTPS://GHCR.io", "acme"}, [2]string{"ghcr.io", "acme"}, true, "ghcr.io/acme"},
		{"http prefix kept", [2]string{"http://localhost:5000", "ocm"}, [2]string{"localhost:5000", "ocm"}, false, "http://localhost:5000/ocm"},
		{"explicit :443 differs from no port", [2]string{"ghcr.io:443", "acme"}, [2]string{"ghcr.io", "acme"}, false, "ghcr.io:443/acme"},
		{"oci:// differs", [2]string{"oci://ghcr.io", "acme"}, [2]string{"ghcr.io", "acme"}, false, "oci://ghcr.io/acme"},
		{"baseUrl path equals subPath", [2]string{"ghcr.io/acme/ocm", ""}, [2]string{"ghcr.io", "acme/ocm"}, true, "ghcr.io/acme/ocm"},
		{"baseUrl path plus subPath", [2]string{"ghcr.io/acme", "ocm"}, [2]string{"ghcr.io", "acme/ocm"}, true, "ghcr.io/acme/ocm"},
		{"subPath case preserved", [2]string{"ghcr.io", "Acme"}, [2]string{"ghcr.io", "acme"}, false, "ghcr.io/Acme"},
		{"baseUrl path case preserved", [2]string{"GHCR.IO/Acme", ""}, [2]string{"ghcr.io", "Acme"}, true, "ghcr.io/Acme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ka, kb := key(tt.a[0], tt.a[1]), key(tt.b[0], tt.b[1])
			if ka != tt.wantKeyOfA {
				t.Errorf("key(%v) = %q, want %q", tt.a, ka, tt.wantKeyOfA)
			}
			if (ka == kb) != tt.equal {
				t.Errorf("key(%v)=%q vs key(%v)=%q: equal=%v, want %v", tt.a, ka, tt.b, kb, ka == kb, tt.equal)
			}
		})
	}
}

// TestRepositoryPinnedNormalisation pins the allow-list comparison: host case
// is irrelevant, port and path case are not. No registry is contacted.
func TestRepositoryPinnedNormalisation(t *testing.T) {
	b := newTestBindings(t, fixtures.WriteOCMConfig(t, []fixtures.Resolver{{Host: "registry.example.com:5000", SubPath: "Team/ocm"}}, nil))
	r, err := NewResolver(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	const want = "http://registry.example.com:5000/Team/ocm"
	if _, key, err := r.Repository(t.Context(), "acme.org/x", "1.0.0", "oci::http://REGISTRY.Example.COM:5000/Team/ocm"); err != nil || key != want {
		t.Fatalf("host differing only in case: key=%q err=%v, want allowed %q", key, err, want)
	}
	for _, pinned := range []string{
		"oci::http://registry.example.com:5001/Team/ocm",  // other port
		"oci::http://registry.example.com/Team/ocm",       // no port
		"oci::http://registry.example.com:5000/team/ocm",  // path case
		"oci::https://registry.example.com:5000/Team/ocm", // other scheme
	} {
		if _, _, err := r.Repository(t.Context(), "acme.org/x", "1.0.0", pinned); !errors.Is(err, ErrRepositoryNotAllowed) {
			t.Errorf("%s: err = %v, want ErrRepositoryNotAllowed", pinned, err)
		}
	}
}

// The UI passes the resolved repository key (Overview.Repository) back as
// ?repository=; every key format must parse to the same key.
func TestResolvedKeyRoundTripsAsPin(t *testing.T) {
	for _, key := range []string{"ghcr.io/acme/ocm", "http://localhost:5000/ocm", "oci://ghcr.io/acme", "ghcr.io:443/acme", "localhost:5000/ocm"} {
		t.Run(key, func(t *testing.T) {
			r := &Resolver{allowed: []string{key}}
			if err := r.CheckRepository(key); err != nil {
				t.Errorf("CheckRepository(%q) = %v", key, err)
			}
		})
	}
}
