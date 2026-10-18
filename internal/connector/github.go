package connector

import (
	"cmp"
	"context"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// descriptorPrefix is the package name prefix of OCM component descriptors
// in an OCI registry (component-descriptors/<component name>).
const descriptorPrefix = "component-descriptors/"

const maxRateLimitWaits = 3

// packagesPerPage is the page size requested from the package list endpoints.
const packagesPerPage = 100

// Owner is whose packages a GitHub connector lists: an organization, a user
// or an enterprise (exactly one).
type Owner struct {
	Org, User, Enterprise string
}

// key is the owner key, e.g. "github:org:acme".
func (o Owner) key() string {
	if o.Enterprise != "" {
		return "github:enterprise:" + strings.ToLower(o.Enterprise)
	}
	if o.User != "" {
		return "github:user:" + strings.ToLower(o.User)
	}
	return "github:org:" + strings.ToLower(o.Org)
}

func (o Owner) name() string {
	if o.User != "" {
		return o.User
	}
	if o.Org != "" {
		return o.Org
	}
	return o.Enterprise
}

// base is the API path of the owner's packages.
func (o Owner) base() string {
	if o.User != "" {
		return "/users/" + url.PathEscape(o.User)
	}
	return "/orgs/" + url.PathEscape(o.Org)
}

// GitHub lists an organization's or user's OCM components via the GitHub Packages
// REST API. The API has no name filter, so all container packages are listed
// and filtered on the prefix here.
type GitHub struct {
	id, token        string
	key, title       string
	owner            Owner
	visibility       []Visibility
	orgs             []OwnerInfo // resolved enterprise orgs
	orgsOK           bool
	apiURL, registry string
	client           *http.Client
	sleep            func(context.Context, time.Duration) error
	now              func() time.Time
}

// GitHubOption configures NewGitHub.
type GitHubOption func(*GitHub)

// WithAPIURL overrides https://api.github.com (GitHub Enterprise Server, tests).
func WithAPIURL(u string) GitHubOption {
	return func(g *GitHub) { g.apiURL = strings.TrimSuffix(u, "/") }
}

// WithRegistry overrides the registry host of the locations (default ghcr.io).
func WithRegistry(host string) GitHubOption { return func(g *GitHub) { g.registry = host } }

// WithVisibility limits discovery to packages with one of the visibilities
// (a single value is also sent to the API as a server-side filter). Without
// it, or with no values, only public packages are indexed.
func WithVisibility(v ...Visibility) GitHubOption {
	return func(g *GitHub) {
		if len(v) > 0 {
			g.visibility = v
		}
	}
}

// WithKey sets the readable source key (Key).
func WithKey(key string) GitHubOption { return func(g *GitHub) { g.key = key } }

// WithTitle sets the configured title override (Title).
func WithTitle(title string) GitHubOption { return func(g *GitHub) { g.title = title } }

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(c *http.Client) GitHubOption { return func(g *GitHub) { g.client = c } }

func withSleep(f func(context.Context, time.Duration) error) GitHubOption {
	return func(g *GitHub) { g.sleep = f }
}

// NewGitHub creates the github/v1 connector for owner (an organization or a user); token may be empty
// (public packages only, low rate limit).
func NewGitHub(id string, owner Owner, token string, opts ...GitHubOption) *GitHub {
	g := &GitHub{id: id, owner: owner, token: token, apiURL: "https://api.github.com", registry: "ghcr.io",
		client: http.DefaultClient, sleep: sleepCtx, now: time.Now, visibility: []Visibility{Public}}
	for _, o := range opts {
		o(g)
	}
	return g
}

func (g *GitHub) ID() string    { return g.id }
func (g *GitHub) Type() string  { return "github/v1" }
func (g *GitHub) Key() string   { return g.key }
func (g *GitHub) Title() string { return g.title }

// Describe reads the owner's profile (GET /orgs/{org} or /users/{user}).
func (g *GitHub) Describe(ctx context.Context) (Source, error) {
	if g.owner.Enterprise != "" {
		src, err := g.describeEnterprise(ctx)
		src.Visibility = g.visibility
		return src, err
	}
	var p struct {
		Name      *string `json:"name"`
		AvatarURL string  `json:"avatar_url"`
	}
	if _, err := g.get(ctx, g.apiURL+g.owner.base(), &p); err != nil {
		return Source{}, fmt.Errorf("owner %s: %w", g.owner.name(), err)
	}
	src := Source{AvatarURL: smallAvatar(p.AvatarURL)}
	if p.Name != nil {
		src.Title = strings.TrimSpace(*p.Name)
	}
	src.Owners = []OwnerInfo{{Key: g.owner.key(), Title: cmp.Or(src.Title, g.owner.name()), AvatarURL: src.AvatarURL}}
	src.Visibility = g.visibility
	return src, nil
}

// smallAvatar keeps only https avatar URLs and requests a 32px image.
func smallAvatar(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return ""
	}
	q := u.Query()
	q.Set("s", "32")
	u.RawQuery = q.Encode()
	return u.String()
}

// repository is the location in ?repository= syntax; subPath (may be empty)
// is the repository sub path below the owner.
func (g *GitHub) repository(owner Owner, subPath string) string {
	r := "oci::" + g.registry + "/" + strings.ToLower(owner.name())
	if subPath != "" {
		r += "/" + subPath
	}
	return r
}

// splitDescriptorPackage splits a package name <subPath>/component-descriptors/<component>
// at the first path segment that is exactly "component-descriptors". subPath
// may be empty or span several segments; ok is false for any other name and
// for an empty component.
func splitDescriptorPackage(name string) (subPath, component string, ok bool) {
	rest := name
	if !strings.HasPrefix(name, descriptorPrefix) {
		i := strings.Index(name, "/"+descriptorPrefix)
		if i <= 0 {
			return "", "", false
		}
		subPath, rest = name[:i], name[i+1:]
	}
	component = strings.TrimPrefix(rest, descriptorPrefix)
	if component == "" {
		return "", "", false
	}
	return subPath, component, true
}

type ghPackage struct {
	Name       string     `json:"name"`
	Visibility Visibility `json:"visibility"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type ghVersion struct {
	CreatedAt time.Time `json:"created_at"`
	Metadata  struct {
		Container struct {
			Tags []string `json:"tags"`
		} `json:"container"`
	} `json:"metadata"`
}

// Changed implements Connector.
func (g *GitHub) Changed(ctx context.Context, since time.Time, cache PageCache) iter.Seq2[ComponentVersions, error] {
	return func(yield func(ComponentVersions, error) bool) {
		if g.owner.Enterprise == "" {
			g.listOwner(ctx, g.owner, since, cache, yield)
			return
		}
		if !g.orgsOK {
			if _, err := g.describeEnterprise(ctx); err != nil {
				yield(ComponentVersions{}, err)
				return
			}
		}
		for _, o := range g.orgs {
			login := strings.TrimPrefix(o.Key, "github:org:")
			ok := g.listOwner(ctx, Owner{Org: login}, since, cache, func(cv ComponentVersions, err error) bool {
				if err != nil {
					return yield(ComponentVersions{}, fmt.Errorf("org %s: %w", login, err))
				}
				return yield(cv, nil)
			})
			if !ok {
				return
			}
		}
	}
}

// listOwner yields owner's changed descriptor packages; false: stop.
func (g *GitHub) listOwner(ctx context.Context, owner Owner, since time.Time, cache PageCache, yield func(ComponentVersions, error) bool) bool {
	next := g.apiURL + owner.base() + "/packages?package_type=container&per_page=" + strconv.Itoa(packagesPerPage)
	if len(g.visibility) == 1 {
		next += "&visibility=" + string(g.visibility[0])
	}
	for next != "" {
		pkgs, link, err := g.listPage(ctx, next, cache) // pkgs is empty on a 304: nothing changed on that page
		if err != nil {
			yield(ComponentVersions{}, err)
			return false
		}
		for _, p := range pkgs {
			subPath, comp, ok := splitDescriptorPackage(p.Name)
			if !ok || !g.visible(p.Visibility) || (!since.IsZero() && !p.UpdatedAt.After(since)) {
				continue
			}
			versions, err := g.versions(ctx, owner, p.Name)
			if err != nil {
				yield(ComponentVersions{}, fmt.Errorf("package %s: %w", p.Name, err))
				return false
			}
			if !yield(ComponentVersions{Component: comp, Repository: g.repository(owner, subPath), Owner: owner.key(),
				Visibility: p.Visibility, Versions: versions}, nil) {
				return false
			}
		}
		next = link
	}
	return true
}

// visible reports whether v passes the configured visibility filter.
func (g *GitHub) visible(v Visibility) bool {
	return len(g.visibility) == 0 || slices.Contains(g.visibility, v)
}

func versionsPath(owner Owner, pkg string) string {
	return owner.base() + "/packages/container/" + url.PathEscape(pkg) + "/versions"
}

// versions returns every tag of every version of pkg; untagged versions
// (e.g. superseded manifests) are skipped.
func (g *GitHub) versions(ctx context.Context, owner Owner, pkg string) ([]Version, error) {
	var out []Version
	next := g.apiURL + versionsPath(owner, pkg) + "?per_page=100"
	for next != "" {
		var page []ghVersion
		link, err := g.get(ctx, next, &page)
		if err != nil {
			return nil, err
		}
		for _, v := range page {
			for _, tag := range v.Metadata.Container.Tags {
				out = append(out, Version{Name: tag, PublishedAt: v.CreatedAt})
			}
		}
		next = link
	}
	return out, nil
}
