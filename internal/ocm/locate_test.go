package ocm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
	"oras.land/oras-go/v2/registry/remote/auth"
)

func TestLocationKey(t *testing.T) {
	for in, want := range map[string]string{
		"ghcr.io/acme":                   "oci::ghcr.io/acme",
		"oci::ghcr.io/acme":              "oci::ghcr.io/acme",
		"oci::https://GHCR.io/acme":      "oci::ghcr.io/acme",
		"oci::http://localhost:5000/ocm": "oci::http://localhost:5000/ocm",
	} {
		got, err := LocationKey(in)
		if err != nil || got != want {
			t.Errorf("LocationKey(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := LocationKey("::::"); !errors.Is(err, ErrRepositoryInvalid) {
		t.Errorf("err = %v", err)
	}
}

// Two sub paths: "main" holds parent+child, "other" holds dep (only reachable
// through the resolver). The parent references child and dep.
func TestDescriptorTreeAtPrefersSelectedLocation(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	sub := fmt.Sprintf("loc-%d", time.Now().UnixNano())
	mainT := fixtures.Target{Host: reg.Host, SubPath: sub + "/main"}
	otherT := fixtures.Target{Host: reg.Host, SubPath: sub + "/other"}
	fixtures.Push(ctx, t, mainT, fixtures.Component{Name: "acme.org/child", Version: "1.0.0"})
	fixtures.Push(ctx, t, otherT, fixtures.Component{Name: "acme.org/child", Version: "1.0.0", Provider: "wrong.org"})
	fixtures.Push(ctx, t, otherT, fixtures.Component{Name: "acme.org/dep", Version: "1.0.0"})
	fixtures.Push(ctx, t, mainT, fixtures.Component{Name: "acme.org/parent", Version: "1.0.0", References: []fixtures.Reference{
		{Name: "child", Component: "acme.org/child", Version: "1.0.0"},
		{Name: "dep", Component: "acme.org/dep", Version: "1.0.0"},
	}})
	// the resolver points everything at "other": child must still come from main
	cfg, err := LoadOCMConfig(fixtures.WriteOCMConfig(t, []fixtures.Resolver{{Pattern: "acme.org/*", Host: reg.Host, SubPath: sub + "/other"}}, nil))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBindings(ctx, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewResolver(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(b, r, 30*time.Second)
	main, _ := LocationKey("oci::http://" + reg.Host + "/" + sub + "/main")
	other, _ := LocationKey("oci::http://" + reg.Host + "/" + sub + "/other")
	has := func(_ context.Context, c, v, loc string) (bool, error) {
		return loc == main && (c == "acme.org/parent" || c == "acme.org/child"), nil
	}
	got, err := svc.DescriptorTreeAt(ctx, Ref{Component: "acme.org/parent", Version: "1.0.0", Repository: main}, has)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"acme.org/parent": main, "acme.org/child": main, "acme.org/dep": other}
	if len(got) != 3 {
		t.Fatalf("got %d descriptors", len(got))
	}
	for _, l := range got {
		if want[l.Descriptor.Component.Name] != l.Location {
			t.Errorf("%s at %s, want %s", l.Descriptor.Component.Name, l.Location, want[l.Descriptor.Component.Name])
		}
		if l.Descriptor.Component.Name == "acme.org/child" && l.Descriptor.Component.Provider.Name == "wrong.org" {
			t.Error("child was fetched from the resolver, not the selected location")
		}
	}
	d, err := svc.DescriptorAt(ctx, "acme.org/dep", "1.0.0", other)
	if err != nil || d.Component.Name != "acme.org/dep" {
		t.Fatalf("DescriptorAt = %v, %v", d, err)
	}
	if _, err := svc.DescriptorAt(ctx, "acme.org/dep", "9.9.9", other); !errors.Is(err, ErrVersionUnknown) {
		t.Fatalf("missing version: %v", err)
	}
}

func TestNewResolverAcceptsNoResolvers(t *testing.T) {
	cfg, err := LoadOCMConfig(fixtures.WriteOCMConfig(t, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBindings(t.Context(), cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewResolver(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve(t.Context(), "acme.org/x", "1.0.0", ""); !errors.Is(err, ErrComponentUnknown) {
		t.Fatalf("err = %v", err)
	}
}

func TestHostCredential(t *testing.T) {
	cred := auth.Credential{Username: "alice", Password: "secret"}
	f := hostCredential("reg.example:5000", cred)
	for host, want := range map[string]auth.Credential{
		"reg.example:5000": cred,
		"reg.example":      auth.EmptyCredential,
		"evil.example":     auth.EmptyCredential,
	} {
		got, err := f(t.Context(), host)
		if err != nil || got != want {
			t.Errorf("credential(%q) = %+v, %v; want %+v", host, got, err, want)
		}
	}
}
