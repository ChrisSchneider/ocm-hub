// Package display derives a component's display data (title, description,
// categories, icon) from catalog.chrisschneider.dev/* labels on its descriptor.
package display

import (
	"encoding/json"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// LabelPrefix is the namespace of the display labels.
const LabelPrefix = "catalog.chrisschneider.dev/"

// Info is a component's display data. Empty fields are unset.
type Info struct {
	Title       string
	Description string
	Categories  []string
	Icon        string // https: or data: URL, or lucide/<name>
}

var iconName = regexp.MustCompile(`^[a-z0-9-]+$`)

// ValidIcon reports whether s is an https: URL with a host, a data: URL, or a
// named icon lucide/<name>.
func ValidIcon(s string) bool {
	switch {
	case strings.HasPrefix(s, "https://"):
		u, err := url.Parse(s)
		return err == nil && u.Host != ""
	case strings.HasPrefix(s, "data:"):
		return len(s) > len("data:")
	}
	lib, name, ok := strings.Cut(s, "/")
	return ok && lib == "lucide" && iconName.MatchString(name)
}

// FromLabels reads the display labels. A label with a value of the wrong type
// (or an invalid icon) is ignored.
func FromLabels(labels []descriptorruntime.Label) Info {
	var in Info
	for _, l := range labels {
		key, ok := strings.CutPrefix(l.Name, LabelPrefix)
		if !ok {
			continue
		}
		var err error
		switch key {
		case "title":
			err = json.Unmarshal(l.Value, &in.Title)
		case "description":
			err = json.Unmarshal(l.Value, &in.Description)
		case "categories":
			var c []string
			if err = json.Unmarshal(l.Value, &c); err == nil {
				in.Categories = nonEmpty(c)
			}
		case "icon":
			var s string
			if err = json.Unmarshal(l.Value, &s); err == nil && ValidIcon(s) {
				in.Icon = s
			} else if err == nil {
				slog.Debug("ignoring invalid icon label", slog.String("value", s))
			}
		}
		if err != nil {
			slog.Debug("ignoring display label with a value of the wrong type", slog.String("label", l.Name), slog.String("error", err.Error()))
		}
	}
	return in
}

func nonEmpty(s []string) []string {
	var out []string
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// WithFallbackTitle sets Title to name if it is empty.
func (in Info) WithFallbackTitle(name string) Info {
	if in.Title == "" {
		in.Title = name
	}
	return in
}
