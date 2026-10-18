package fixtures_test

import (
	"bytes"
	"testing"

	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
	"ocm.software/open-component-model/bindings/go/blob"
)

func TestMain(m *testing.M) { zot.Main(m) }

func TestPushAndReadBack(t *testing.T) {
	reg := zot.Shared(t)
	ctx := t.Context()
	target := fixtures.Target{Host: reg.Host, SubPath: "smoke"}
	fixtures.Push(ctx, t, target, fixtures.Component{
		Name: "acme.org/smoke", Version: "1.0.0",
		Resources: []fixtures.Resource{{Name: "data", Type: "plainText", MediaType: "text/plain", Content: []byte("hello")}},
	})

	repo := fixtures.Repository(t, target)
	desc, err := repo.GetComponentVersion(ctx, "acme.org/smoke", "1.0.0")
	if err != nil {
		t.Fatalf("GetComponentVersion: %v", err)
	}
	if got := len(desc.Component.Resources); got != 1 {
		t.Fatalf("resources = %d, want 1", got)
	}
	data, _, err := repo.GetLocalResource(ctx, "acme.org/smoke", "1.0.0", desc.Component.Resources[0].ToIdentity())
	if err != nil {
		t.Fatalf("GetLocalResource: %v", err)
	}
	var buf bytes.Buffer
	if err := blob.Copy(&buf, data); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello" {
		t.Fatalf("content = %q, want hello", buf.String())
	}
}
