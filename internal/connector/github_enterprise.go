package connector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// enterpriseQuery lists an enterprise's organizations (GraphQL; needs read:enterprise).
const enterpriseQuery = `query($slug: String!, $after: String) {
  enterprise(slug: $slug) {
    name
    avatarUrl
    organizations(first: 100, after: $after) {
      nodes { login name avatarUrl }
      pageInfo { hasNextPage endCursor }
    }
  }
}`

type gqlEnterprise struct {
	Data struct {
		Enterprise *struct {
			Name          string `json:"name"`
			AvatarURL     string `json:"avatarUrl"`
			Organizations struct {
				Nodes []struct {
					Login     string  `json:"login"`
					Name      *string `json:"name"`
					AvatarURL string  `json:"avatarUrl"`
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool    `json:"hasNextPage"`
					EndCursor   *string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"organizations"`
		} `json:"enterprise"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// describeEnterprise lists the enterprise's orgs (all pages), remembers them
// for Changed and returns the enterprise as source.
func (g *GitHub) describeEnterprise(ctx context.Context) (Source, error) {
	slug := g.owner.Enterprise
	var src Source
	var orgs []OwnerInfo
	var logins []string
	var after *string
	for {
		var res gqlEnterprise
		if err := g.graphql(ctx, enterpriseQuery, map[string]any{"slug": slug, "after": after}, &res); err != nil {
			return Source{}, err
		}
		if len(res.Errors) > 0 {
			msgs := make([]string, len(res.Errors))
			for i, e := range res.Errors {
				msgs[i] = e.Message
			}
			return Source{}, fmt.Errorf("enterprise %s: %s", slug, strings.Join(msgs, "; "))
		}
		ent := res.Data.Enterprise
		if ent == nil {
			return Source{}, fmt.Errorf("enterprise %s not visible: the GitHub token needs read:enterprise and enterprise membership", slug)
		}
		src.Title, src.AvatarURL = strings.TrimSpace(ent.Name), smallAvatar(ent.AvatarURL)
		for _, n := range ent.Organizations.Nodes {
			title := n.Login
			if n.Name != nil && strings.TrimSpace(*n.Name) != "" {
				title = strings.TrimSpace(*n.Name)
			}
			orgs = append(orgs, OwnerInfo{Key: "github:org:" + strings.ToLower(n.Login), Title: title,
				AvatarURL: smallAvatar(n.AvatarURL), Parent: g.owner.key()})
			logins = append(logins, n.Login)
		}
		if !ent.Organizations.PageInfo.HasNextPage {
			break
		}
		next := ent.Organizations.PageInfo.EndCursor
		if next == nil || *next == "" || (after != nil && *next == *after) {
			return Source{}, fmt.Errorf("enterprise %s: GraphQL paging did not advance", slug)
		}
		after = next
	}
	g.orgs, g.orgsOK = orgs, true
	src.Owners, src.Scope = orgs, scopeOf(logins)
	return src, nil
}

// scopeOf fingerprints a set of logins (order and case insensitive).
func scopeOf(logins []string) string {
	l := make([]string, len(logins))
	for i, s := range logins {
		l[i] = strings.ToLower(s)
	}
	slices.Sort(l)
	sum := sha256.Sum256([]byte(strings.Join(l, "\n")))
	return hex.EncodeToString(sum[:])
}

// graphql POSTs query to {apiURL}/graphql and decodes the response into out.
func (g *GitHub) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	u := g.apiURL + "/graphql"
	resp, err := g.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, err
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeOK(resp, u, out)
}
