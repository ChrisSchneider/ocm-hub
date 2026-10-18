package ocm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmrepository "ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	"oras.land/oras-go/v2/registry/remote/errcode"
)

func res(name string, extra map[string]string) descriptorruntime.Resource {
	return descriptorruntime.Resource{ElementMeta: descriptorruntime.ElementMeta{
		ObjectMeta:    descriptorruntime.ObjectMeta{Name: name, Version: "1.0.0"},
		ExtraIdentity: ocmruntime.Identity(extra),
	}}
}

func TestSelectResource(t *testing.T) {
	all := []descriptorruntime.Resource{
		res("data", nil),
		res("bin", map[string]string{"os": "linux", "arch": "amd64"}),
		res("bin", map[string]string{"os": "linux", "arch": "arm64"}),
	}
	if r, err := selectResource(all, ocmruntime.Identity{"name": "data"}); err != nil || r.Name != "data" {
		t.Fatalf("data: %v %v", r, err)
	}
	if r, err := selectResource(all, ocmruntime.Identity{"name": "bin", "arch": "arm64"}); err != nil || r.ExtraIdentity["arch"] != "arm64" {
		t.Fatalf("bin arm64: %v %v", r, err)
	}
	_, err := selectResource(all, ocmruntime.Identity{"name": "bin"})
	var amb *AmbiguousResourceError
	if !errors.Is(err, ErrResourceAmbiguous) || !errors.As(err, &amb) || len(amb.Candidates) != 2 {
		t.Fatalf("bin: err = %v, want ambiguous with 2 candidates", err)
	}
	if _, err := selectResource(all, ocmruntime.Identity{"name": "nope"}); !errors.Is(err, ErrResourceUnknown) {
		t.Fatalf("nope: err = %v", err)
	}
}

func TestResourceDigest(t *testing.T) {
	r := res("x", nil)
	if got := resourceDigest(&r); got != "" {
		t.Fatalf("no digest: %q", got)
	}
	r.Digest = &descriptorruntime.Digest{HashAlgorithm: "SHA-256", Value: "abc"}
	if got := resourceDigest(&r); got != "sha256:abc" {
		t.Fatalf("got %q, want sha256:abc", got)
	}
}

func newTestService(t *testing.T, resolverList []fixtures.Resolver, creds []fixtures.Credential) *Service {
	t.Helper()
	b := newTestBindings(t, fixtures.WriteOCMConfig(t, resolverList, creds))
	r, err := NewResolver(t.Context(), b)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return NewService(b, r, 30*time.Second)
}

func TestServiceAgainstZot(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host, SubPath: "svc"}
	files := fixtures.FileServer(t, map[string][]byte{"/f": []byte("remote content")})
	resources := []fixtures.Resource{
		{Name: "data", Type: "plainText", MediaType: "text/plain", Content: []byte("payload")},
		{Name: "bin", Type: "executable", MediaType: "application/octet-stream", Content: []byte("amd64-bits"), ExtraIdentity: map[string]string{"os": "linux", "arch": "amd64"}},
		{Name: "bin", Type: "executable", MediaType: "application/octet-stream", Content: []byte("arm64-bits"), ExtraIdentity: map[string]string{"os": "linux", "arch": "arm64"}},
		{Name: "remote", Type: "blob", Access: &wgetaccessv1.Wget{Type: ocmruntime.NewVersionedType("Wget", wgetaccessv1.Version), URL: files.URL + "/f"}},
		{Name: "exotic", Type: "blob", Access: &ocmruntime.Raw{Type: ocmruntime.NewVersionedType("doesNotExist", "v1"), Data: []byte(`{"type":"doesNotExist/v1"}`)}},
	}
	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0-rc.1"} {
		fixtures.Push(ctx, t, target, fixtures.Component{Name: "acme.org/svc", Version: v, Resources: resources})
	}
	s := newTestService(t, []fixtures.Resolver{{Pattern: "acme.org/*", Host: reg.Host, SubPath: "svc"}}, nil)
	ref := Ref{Component: "acme.org/svc", Version: "1.1.0"}

	t.Run("GetComponentVersion", func(t *testing.T) {
		d, err := s.GetComponentVersion(ctx, ref)
		if err != nil || d.Component.Name != "acme.org/svc" || d.Component.Version != "1.1.0" {
			t.Fatalf("desc=%v err=%v", d, err)
		}
	})
	t.Run("at a location", func(t *testing.T) {
		loc, err := LocationKey("oci::http://" + reg.Host + "/svc")
		if err != nil {
			t.Fatal(err)
		}
		d, err := s.GetComponentVersion(ctx, Ref{Component: "acme.org/svc", Version: "2.0.0-rc.1", Repository: loc})
		if err != nil || d.Component.Version != "2.0.0-rc.1" {
			t.Fatalf("desc=%v err=%v", d, err)
		}
		r, err := s.OpenResource(ctx, Ref{Component: "acme.org/svc", Version: "1.0.0", Repository: loc}, ocmruntime.Identity{"name": "data"}, false)
		if err != nil || r.Name != "data" {
			t.Fatalf("resource=%+v err=%v", r, err)
		}
		if _, err := s.GetComponentVersion(ctx, Ref{Component: "acme.org/svc", Version: "9.9.9", Repository: loc}); !errors.Is(err, ErrVersionUnknown) {
			t.Fatalf("unknown version at location: err = %v", err)
		}
	})
	t.Run("unknown version", func(t *testing.T) {
		if _, err := s.GetComponentVersion(ctx, Ref{Component: "acme.org/svc", Version: "9.9.9"}); !errors.Is(err, ErrVersionUnknown) {
			t.Fatalf("err = %v, want ErrVersionUnknown", err)
		}
	})
	t.Run("unknown component", func(t *testing.T) {
		if _, err := s.GetComponentVersion(ctx, Ref{Component: "acme.org/missing", Version: "1.0.0"}); !errors.Is(err, ErrComponentUnknown) {
			t.Fatalf("err = %v, want ErrComponentUnknown", err)
		}
	})
	open := func(t *testing.T, id ocmruntime.Identity) (*Resource, []byte) {
		t.Helper()
		r, err := s.OpenResource(ctx, ref, id, true)
		if err != nil {
			t.Fatalf("OpenResource(%v): %v", id, err)
		}
		defer r.Body.Close()
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		return r, data
	}
	t.Run("local blob", func(t *testing.T) {
		r, data := open(t, ocmruntime.Identity{"name": "data"})
		if !bytes.Equal(data, []byte("payload")) || r.Name != "data" {
			t.Fatalf("data=%q res=%+v", data, r)
		}
		if r.Size >= 0 && r.Size != int64(len("payload")) {
			t.Fatalf("size = %d", r.Size)
		}
		if r.Digest == "" || !r.DigestMatchesBody {
			t.Fatalf("localBlob digest=%q matchesBody=%v, want a digest matching the body", r.Digest, r.DigestMatchesBody)
		}
	})
	t.Run("without body", func(t *testing.T) {
		for _, name := range []string{"data", "remote"} {
			r, err := s.OpenResource(ctx, ref, ocmruntime.Identity{"name": name}, false)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if r.Body != nil || r.Name != name {
				t.Fatalf("%s: body must be nil without body: %+v", name, r)
			}
		}
	})
	t.Run("extra identity selects", func(t *testing.T) {
		_, data := open(t, ocmruntime.Identity{"name": "bin", "arch": "arm64"})
		if string(data) != "arm64-bits" {
			t.Fatalf("data=%q", data)
		}
	})
	t.Run("external wget", func(t *testing.T) {
		r, data := open(t, ocmruntime.Identity{"name": "remote"})
		if string(data) != "remote content" {
			t.Fatalf("data=%q", data)
		}
		if r.DigestMatchesBody {
			t.Fatal("a wget resource's OCM digest must not be treated as a body hash")
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		if _, err := s.OpenResource(ctx, ref, ocmruntime.Identity{"name": "bin"}, true); !errors.Is(err, ErrResourceAmbiguous) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unsupported access type", func(t *testing.T) {
		if _, err := s.OpenResource(ctx, ref, ocmruntime.Identity{"name": "exotic"}, true); !errors.Is(err, ErrAccessTypeUnsupported) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestServiceCredentials(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := t.Context()
	reg, err := zot.Start(ctx, zot.WithHtpasswd("alice", "s3cret"))
	if err != nil {
		t.Fatalf("zot: %v", err)
	}
	t.Cleanup(func() { _ = reg.Stop(ctx) })
	target := fixtures.Target{Host: reg.Host, SubPath: "auth", Username: "alice", Password: "s3cret"}
	fixtures.Push(ctx, t, target, fixtures.Component{Name: "acme.org/auth", Version: "1.0.0",
		Resources: []fixtures.Resource{{Name: "d", Type: "plainText", MediaType: "text/plain", Content: []byte("x")}}})
	resolverList := []fixtures.Resolver{{Host: reg.Host, SubPath: "auth"}}
	ref := Ref{Component: "acme.org/auth", Version: "1.0.0"}

	withCreds := newTestService(t, resolverList, []fixtures.Credential{{Host: reg.Host, Username: "alice", Password: "s3cret"}})
	if _, err := withCreds.GetComponentVersion(ctx, ref); err != nil {
		t.Fatalf("with credentials: %v", err)
	}
	without := newTestService(t, resolverList, nil)
	if _, err := without.GetComponentVersion(ctx, ref); !errors.Is(err, ErrUpstream) {
		t.Fatalf("without credentials: err = %v, want ErrUpstream", err)
	}
	if _, err := without.GetComponentVersion(ctx, ref); errors.Is(err, ErrComponentUnknown) {
		t.Fatalf("without credentials: err = %v, must not be reported as unknown", err)
	}
}

func TestIsNotFound(t *testing.T) {
	resp := func(code int) error {
		return fmt.Errorf("wrapped: %w", &errcode.ErrorResponse{StatusCode: code})
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"wrapped ErrNotFound", fmt.Errorf("x: %w", ocmrepository.ErrNotFound), true},
		{"404", resp(404), true},
		{"401", resp(401), false},
		{"403", resp(403), false},
		{"500", resp(500), false},
		{"plain", errors.New("boom"), false},
		{"deadline", context.DeadlineExceeded, false},
	} {
		if got := isNotFound(tc.err); got != tc.want {
			t.Errorf("%s: isNotFound = %v, want %v", tc.name, got, tc.want)
		}
	}
}

type fakeRepo struct {
	ocmrepository.ComponentVersionRepository
	getErr, listErr error
	versions        []string
}

func (f fakeRepo) GetComponentVersion(context.Context, string, string) (*descriptorruntime.Descriptor, error) {
	return nil, f.getErr
}

func (f fakeRepo) ListComponentVersions(context.Context, string) ([]string, error) {
	return f.versions, f.listErr
}

func TestDescriptorClassification(t *testing.T) {
	notFound := fmt.Errorf("x: %w", ocmrepository.ErrNotFound)
	ref := Ref{Component: "c", Version: "1.0.0"}
	for _, tc := range []struct {
		name string
		repo fakeRepo
		want error
	}{
		{"get upstream", fakeRepo{getErr: errors.New("boom")}, ErrUpstream},
		{"get timeout", fakeRepo{getErr: context.DeadlineExceeded}, ErrUpstreamTimeout},
		{"list upstream failure is not 404", fakeRepo{getErr: notFound, listErr: &errcode.ErrorResponse{StatusCode: 401}}, ErrUpstream},
		{"list timeout is not 404", fakeRepo{getErr: notFound, listErr: context.DeadlineExceeded}, ErrUpstreamTimeout},
		{"list not found", fakeRepo{getErr: notFound, listErr: &errcode.ErrorResponse{StatusCode: 404}}, ErrComponentUnknown},
		{"list empty", fakeRepo{getErr: notFound}, ErrComponentUnknown},
		{"version missing", fakeRepo{getErr: notFound, versions: []string{"2.0.0"}}, ErrVersionUnknown},
	} {
		_, err := getDescriptor(t.Context(), tc.repo, ref)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if tc.want != ErrVersionUnknown && tc.want != ErrComponentUnknown && (errors.Is(err, ErrVersionUnknown) || errors.Is(err, ErrComponentUnknown)) {
			t.Errorf("%s: reported as 404: %v", tc.name, err)
		}
	}
}

// TestOpenResourceReleasesWgetTempFile: the wget binding downloads into a temp
// file owned by the blob; closing the body (or opening without body) must
// remove it instead of leaving it to the garbage collector.
func TestOpenResourceReleasesWgetTempFile(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host, SubPath: "wget-cleanup"}
	files := fixtures.FileServer(t, map[string][]byte{"/f": []byte("remote content")})
	fixtures.Push(ctx, t, target, fixtures.Component{Name: "acme.org/wget", Version: "1.0.0", Resources: []fixtures.Resource{
		{Name: "remote", Type: "blob", Access: &wgetaccessv1.Wget{Type: ocmruntime.NewVersionedType("Wget", wgetaccessv1.Version), URL: files.URL + "/f"}},
	}})
	cfg, err := LoadOCMConfig(fixtures.WriteOCMConfig(t, []fixtures.Resolver{{Pattern: "acme.org/*", Host: reg.Host, SubPath: "wget-cleanup"}}, nil))
	if err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	b, err := NewBindings(ctx, cfg, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close(context.Background()) })
	r, err := NewResolver(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(b, r, 30*time.Second)
	ref := Ref{Component: "acme.org/wget", Version: "1.0.0"}
	leftovers := func(t *testing.T) []string {
		t.Helper()
		entries, err := os.ReadDir(tempDir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}

	res, err := s.OpenResource(ctx, ref, ocmruntime.Identity{"name": "remote"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers(t)) == 0 {
		t.Fatal("expected the wget download in the bindings temp dir while the body is open")
	}
	data, err := io.ReadAll(res.Body)
	if err != nil || string(data) != "remote content" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if err := res.Body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if l := leftovers(t); len(l) != 0 {
		t.Fatalf("temp files left after close: %v", l)
	}

	if _, err := s.OpenResource(ctx, ref, ocmruntime.Identity{"name": "remote"}, false); err != nil {
		t.Fatal(err)
	}
	if l := leftovers(t); len(l) != 0 {
		t.Fatalf("temp files left after opening without body: %v", l)
	}
}
