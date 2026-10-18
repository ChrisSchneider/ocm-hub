package overview

import "testing"

func TestNamePrefix(t *testing.T) {
	for name, want := range map[string]string{
		"ocm.software/plugins/ecrplugin":                 "ocm.software",
		"helm.sh/helm":                                   "helm.sh",
		"github.com/chrisschneider/opencode-base-images": "github.com/chrisschneider",
		"github.com/acme":                                "github.com/acme",
		"gitlab.com/acme/app":                            "gitlab.com/acme",
		"example.org":                                    "example.org",
	} {
		if got := NamePrefix(name, DefaultCodeHosts); got != want {
			t.Errorf("NamePrefix(%q) = %q, want %q", name, got, want)
		}
	}
	if got := NamePrefix("git.acme.corp/team/app", []string{"git.acme.corp"}); got != "git.acme.corp/team" {
		t.Errorf("custom host: %q", got)
	}
}
