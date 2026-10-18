package fixtures

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Resolver is one resolvers.config.ocm.software/v1alpha1 entry for a plain-HTTP
// OCI repository.
type Resolver struct {
	Pattern    string // componentNamePattern; empty = all
	Constraint string // versionConstraint; empty = none
	Host       string // host:port
	SubPath    string
}

// Credential is a username/password for an OCI registry host.
type Credential struct {
	Host     string // host:port
	Username string
	Password string
}

// WriteOCMConfig writes a standard .ocmconfig with the given resolvers and
// credentials and returns its path.
func WriteOCMConfig(t testing.TB, resolvers []Resolver, creds []Credential) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("type: generic.config.ocm.software/v1\nconfigurations:\n")
	if len(resolvers) > 0 {
		b.WriteString("- type: resolvers.config.ocm.software/v1alpha1\n  resolvers:\n")
	}
	for _, r := range resolvers {
		fmt.Fprintf(&b, "  - repository:\n      type: OCIRepository/v1\n      baseUrl: http://%s\n", r.Host)
		if r.SubPath != "" {
			fmt.Fprintf(&b, "      subPath: %s\n", r.SubPath)
		}
		// The bindings compile an absent pattern as the empty glob, which
		// matches no component; "empty = all" therefore needs an explicit "*".
		pattern := r.Pattern
		if pattern == "" {
			pattern = "*"
		}
		fmt.Fprintf(&b, "    componentNamePattern: %q\n", pattern)
		if r.Constraint != "" {
			fmt.Fprintf(&b, "    versionConstraint: %q\n", r.Constraint)
		}
	}
	if len(creds) > 0 {
		b.WriteString("- type: credentials.config.ocm.software\n  consumers:\n")
		for _, c := range creds {
			host, port, err := net.SplitHostPort(c.Host)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "  - identity:\n      type: OCIRegistry\n      hostname: %s\n      port: %q\n", host, port)
			fmt.Fprintf(&b, "    credentials:\n    - type: Credentials/v1\n      properties:\n        username: %s\n        password: %s\n", c.Username, c.Password)
		}
	}
	path := filepath.Join(t.TempDir(), ".ocmconfig")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
