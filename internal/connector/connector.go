// Package connector finds OCM components and their versions through the
// native APIs of the services that host them.
package connector

import (
	"context"
	"iter"
	"time"
)

// Version is an OCM version and when it was published at a location.
type Version struct {
	Name        string
	PublishedAt time.Time
}

// Visibility is who may see a package at its location.
type Visibility string

const (
	Public   Visibility = "public"
	Internal Visibility = "internal"
	Private  Visibility = "private"
)

// PageCache remembers the ETag and next link of list pages for conditional
// requests.
type PageCache interface {
	Lookup(url string) (etag, next string, ok bool)
	Store(url, etag, next string)
}

// ComponentVersions is the complete version list of a component at one
// location (repository in ?repository= syntax, e.g. "oci::ghcr.io/acme").
type ComponentVersions struct {
	Component  string
	Repository string
	Owner      string // owner key of the location ("github:org:acme")
	Visibility Visibility
	Versions   []Version
}

// Connector is one discovery: entry of hub.yaml.
type Connector interface {
	ID() string
	Type() string
	// Key is the readable source identity (e.g. "github:org:acme").
	Key() string
	// Title is the configured title override (hub.yaml name:), "" if none.
	Title() string
	// Changed yields every component whose versions changed after since
	// (zero: all), each with its complete version list. After an error it
	// yields nothing more. cache may be nil; connectors that support it use
	// it for conditional requests.
	Changed(ctx context.Context, since time.Time, cache PageCache) iter.Seq2[ComponentVersions, error]
}

// Source is how a connector's owner presents itself.
type Source struct {
	Title, AvatarURL string       // either may be empty
	Scope            string       // fingerprint of the listed owners; "" for a fixed owner
	Owners           []OwnerInfo  // every owner listed; nil if unknown
	Visibility       []Visibility // indexed visibilities (default: public only)
}

// OwnerInfo describes one owner a source lists.
type OwnerInfo struct {
	Key, Title, AvatarURL string
	Parent                string // enterprise key, "" if none
}

// Describer reports a source's display name and avatar.
type Describer interface {
	Describe(ctx context.Context) (Source, error)
}
