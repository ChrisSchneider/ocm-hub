package fixtures

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"sigs.k8s.io/yaml"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

type constructorFile struct {
	Components []struct {
		Name     string `json:"name"`
		Version  string `json:"version"`
		Provider struct {
			Name string `json:"name"`
		} `json:"provider"`
		Labels  []descriptorruntime.Label `json:"labels"`
		Sources []struct {
			Name    string          `json:"name"`
			Type    string          `json:"type"`
			Version string          `json:"version"`
			Access  json.RawMessage `json:"access"`
		} `json:"sources"`
		Resources []struct {
			Name    string                    `json:"name"`
			Type    string                    `json:"type"`
			Version string                    `json:"version"`
			Labels  []descriptorruntime.Label `json:"labels"`
			Access  *struct {
				Type           string `json:"type"`
				ImageReference string `json:"imageReference"`
			} `json:"access"`
			Input *struct {
				Type      string `json:"type"`
				Path      string `json:"path"`
				MediaType string `json:"mediaType"`
			} `json:"input"`
		} `json:"resources"`
	} `json:"components"`
}

var varPattern = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)

// LoadConstructor reads an OCM component constructor file into fixture
// components. It supports only what the examples use: labels, sources (access kept raw), File/v1 inputs
// (paths relative to the file), OCIImage/v1 accesses and ${VAR} substitution;
// anything else, including an unset variable, fails the test.
func LoadConstructor(t testing.TB, path string, vars map[string]string) []Component {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = varPattern.ReplaceAllFunc(raw, func(m []byte) []byte {
		v, ok := vars[string(varPattern.FindSubmatch(m)[1])]
		if !ok {
			t.Fatalf("constructor %s: variable %s is not set", path, m)
		}
		return []byte(v)
	})
	js, err := yaml.YAMLToJSON(raw)
	if err != nil {
		t.Fatalf("constructor %s: %v", path, err)
	}
	var f constructorFile
	dec := json.NewDecoder(bytes.NewReader(js))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		t.Fatalf("constructor %s: %v", path, err)
	}
	var out []Component
	for _, c := range f.Components {
		comp := Component{Name: c.Name, Version: c.Version, Provider: c.Provider.Name, Labels: c.Labels}
		for _, src := range c.Sources {
			var typ struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(src.Access, &typ); err != nil || typ.Type == "" {
				t.Fatalf("constructor %s: source %s: access needs a type", path, src.Name)
			}
			comp.Sources = append(comp.Sources, descriptorruntime.Source{
				ElementMeta: descriptorruntime.ElementMeta{ObjectMeta: descriptorruntime.ObjectMeta{Name: src.Name, Version: src.Version}},
				Type:        src.Type,
				Access:      &ocmruntime.Raw{Type: ocmruntime.NewUnversionedType(typ.Type), Data: src.Access},
			})
		}
		for _, r := range c.Resources {
			res := Resource{Name: r.Name, Type: r.Type, Version: r.Version, Labels: r.Labels}
			switch {
			case r.Access != nil && r.Input == nil && r.Access.Type == "OCIImage/v1":
				res.Access = &ociaccessv1.OCIImage{Type: ocmruntime.NewVersionedType("OCIImage", "v1"), ImageReference: r.Access.ImageReference}
			case r.Input != nil && r.Access == nil && r.Input.Type == "File/v1":
				res.Path, res.MediaType = filepath.Join(filepath.Dir(path), r.Input.Path), r.Input.MediaType
			default:
				t.Fatalf("constructor %s: resource %s: unsupported access/input", path, r.Name)
			}
			comp.Resources = append(comp.Resources, res)
		}
		out = append(out, comp)
	}
	return out
}
