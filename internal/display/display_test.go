package display

import (
	"encoding/json"
	"reflect"
	"testing"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func label(name string, value any) descriptorruntime.Label {
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return descriptorruntime.Label{Name: name, Value: b}
}

func TestFromLabels(t *testing.T) {
	got := FromLabels([]descriptorruntime.Label{
		label(LabelPrefix+"title", "How-to Product"),
		label(LabelPrefix+"description", "Bundle of backend, frontend and cache."),
		label(LabelPrefix+"categories", []string{"examples", "", "demo"}),
		label(LabelPrefix+"icon", "lucide/phone"),
		label("other.org/title", "ignored"),
	})
	want := Info{Title: "How-to Product", Description: "Bundle of backend, frontend and cache.", Categories: []string{"examples", "demo"}, Icon: "lucide/phone"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFromLabelsIgnoresWrongTypes(t *testing.T) {
	got := FromLabels([]descriptorruntime.Label{
		label(LabelPrefix+"title", 42),
		label(LabelPrefix+"description", map[string]string{"en": "x"}),
		label(LabelPrefix+"categories", "examples"),
		label(LabelPrefix+"icon", "ftp://example.com/x.svg"),
	})
	if !reflect.DeepEqual(got, Info{}) {
		t.Fatalf("got %+v, want zero Info", got)
	}
}

func TestValidIcon(t *testing.T) {
	for icon, want := range map[string]bool{
		"https://example.com/a.svg":          true,
		"https://":                           false,
		"http://example.com/a.svg":           false,
		"data:image/svg+xml;base64,PHN2Zy8+": true,
		"data:":                              false,
		"lucide/phone":                       true,
		"lucide/arrow-up-right":              true,
		"lucide/Phone":                       false,
		"lucide/":                            false,
		"lucide/a/b":                         false,
		"heroicons/phone":                    false,
		"phone":                              false,
		"":                                   false,
	} {
		if got := ValidIcon(icon); got != want {
			t.Errorf("ValidIcon(%q) = %v, want %v", icon, got, want)
		}
	}
}

func TestFallbackTitle(t *testing.T) {
	base := Info{Title: "Label"}
	if got := (Info{}).WithFallbackTitle("acme.org/x").Title; got != "acme.org/x" {
		t.Fatalf("fallback title = %q", got)
	}
	if got := base.WithFallbackTitle("acme.org/x").Title; got != "Label" {
		t.Fatalf("fallback must not replace a title, got %q", got)
	}
}
