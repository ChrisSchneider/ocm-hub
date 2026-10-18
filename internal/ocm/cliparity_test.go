package ocm

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"chrisschneider.dev/delivery-bundler/internal/testutil/fixtures"
	"chrisschneider.dev/delivery-bundler/internal/testutil/zot"
)

const howToConstructor = `components:
- name: ocm.software/how-to/product
  version: 2.0.0
  provider: {name: ocm.software}
  componentReferences:
  - {name: backend, componentName: ocm.software/how-to/backend, version: 3.1.0}
  - {name: frontend, componentName: ocm.software/how-to/frontend, version: 1.5.0}
  - {name: cache, componentName: ocm.software/how-to/cache, version: 2.0.0}
- name: ocm.software/how-to/backend
  version: 3.1.0
  provider: {name: ocm.software}
  componentReferences:
  - {name: cache, componentName: ocm.software/how-to/cache, version: 2.0.0}
- name: ocm.software/how-to/frontend
  version: 1.5.0
  provider: {name: ocm.software}
- name: ocm.software/how-to/cache
  version: 2.0.0
  provider: {name: ocm.software}
`

// TestDescriptorTreeMatchesCLI compares DescriptorTreeAt with
// `ocm get cv --recursive -o json` (duplicates removed) for the same registry.
func TestDescriptorTreeMatchesCLI(t *testing.T) {
	ocmBin, err := exec.LookPath("ocm")
	if err != nil {
		t.Skip("ocm CLI not on PATH")
	}
	reg := zot.Shared(t)
	dir := t.TempDir()
	constructor := filepath.Join(dir, "component-constructor.yaml")
	config := filepath.Join(dir, "ocmconfig.yaml")
	if err := os.WriteFile(constructor, []byte(howToConstructor), 0o600); err != nil {
		t.Fatal(err)
	}
	// An explicit, empty config keeps the user's ~/.ocm/config out of the test.
	if err := os.WriteFile(config, []byte("type: generic.config.ocm.software/v1\nconfigurations: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := "oci::http://" + reg.Host + "/cli-parity"
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), ocmBin, append([]string{"--config", config}, args...)...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			var stderr []byte
			if ee, ok := err.(*exec.ExitError); ok {
				stderr = ee.Stderr
			}
			t.Fatalf("ocm %v: %v\n%s", args, err, stderr)
		}
		return out
	}
	run("add", "cv", "--repository", repo, "--constructor", constructor)
	var cli []struct {
		Component struct{ Name, Version string } `json:"component"`
	}
	if err := json.Unmarshal(run("get", "cv", repo+"//ocm.software/how-to/product:2.0.0", "--recursive", "-o", "json"), &cli); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, d := range cli {
		if id := d.Component.Name + ":" + d.Component.Version; !slices.Contains(want, id) {
			want = append(want, id)
		}
	}

	svc := newTestService(t, []fixtures.Resolver{{Host: reg.Host, SubPath: "cli-parity"}}, nil)
	loc, err := LocationKey("oci::http://" + reg.Host + "/cli-parity")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.DescriptorTreeAt(t.Context(), Ref{Component: "ocm.software/how-to/product", Version: "2.0.0", Repository: loc}, everywhere)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(got), want) {
		t.Fatalf("DescriptorTreeAt %v, CLI (deduplicated) %v", ids(got), want)
	}
}
