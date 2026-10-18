// Package index is the DuckDB index of components, versions and locations.
// cmd/sync writes it (Writer); the server reads it (Store, Handle).
package index

import (
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/duckdb/duckdb-go/v2" // registers the "duckdb" driver
)

// SchemaVersion is the version of the schema below. Bump it on every change:
// the next sync then discards the old file and runs a full sync.
const SchemaVersion = 9

var (
	// ErrSchemaMismatch: the file was written with another SchemaVersion.
	ErrSchemaMismatch = errors.New("index schema version mismatch")
	// ErrUnavailable: there is no usable index file (missing or mismatched).
	ErrUnavailable = errors.New("index unavailable")
	// ErrNotFound: the component is not in the index.
	ErrNotFound = errors.New("not in index")
)

// version_changelogs is a side table: component_versions rows are rewritten by
// connector syncs, and the flag is keyed by (component, version) instead. It has
// a PRIMARY KEY and is only ever upserted, never deleted and re-inserted.
//
// component_versions, component_references, discoveries, http_cache and the attestation tables have no PRIMARY KEY on purpose: DuckDB
// checks unique constraints eagerly, so deleting and re-inserting the same key
// in one transaction fails. Uniqueness is kept by delete-then-insert.
var ddl = []string{
	`CREATE TABLE schema_version (version INTEGER NOT NULL)`,
	`CREATE SEQUENCE location_ids START 1`,
	`CREATE TABLE locations (
		id INTEGER PRIMARY KEY DEFAULT nextval('location_ids'),
		repository VARCHAR NOT NULL UNIQUE)`,
	`CREATE TABLE components (
		name VARCHAR PRIMARY KEY,
		latest_version VARCHAR,
		latest_published_at TIMESTAMPTZ,
		default_version VARCHAR,
		title VARCHAR NOT NULL,
		description VARCHAR,
		icon VARCHAR,
		categories VARCHAR[],
		architectures VARCHAR[],
		provider VARCHAR,
		signed BOOLEAN NOT NULL DEFAULT false,
		name_prefix VARCHAR,
		top_level BOOLEAN NOT NULL DEFAULT true,
		relevance INTEGER NOT NULL DEFAULT 0,
		overview_location_id INTEGER,
		overview_fetched_at TIMESTAMPTZ)`,
	`CREATE TABLE component_versions (
		component VARCHAR NOT NULL,
		location_id INTEGER NOT NULL,
		version VARCHAR NOT NULL,
		published_at TIMESTAMPTZ NOT NULL,
		stable BOOLEAN NOT NULL)`,
	`CREATE TABLE component_references (
		parent_name VARCHAR NOT NULL,
		parent_version VARCHAR NOT NULL,
		reference_name VARCHAR NOT NULL,
		child_name VARCHAR NOT NULL,
		child_version VARCHAR NOT NULL)`,
	`CREATE TABLE discoveries (
		connector_id VARCHAR NOT NULL,
		location_id INTEGER NOT NULL,
		component VARCHAR NOT NULL,
		owner_key VARCHAR NOT NULL,
		visibility VARCHAR NOT NULL,
		last_seen_at TIMESTAMPTZ NOT NULL)`,
	`CREATE TABLE connectors (
		id VARCHAR PRIMARY KEY,
		type VARCHAR NOT NULL,
		source_key VARCHAR NOT NULL UNIQUE,
		title VARCHAR NOT NULL,
		avatar_url VARCHAR,
		scope VARCHAR,
		position INTEGER NOT NULL,
		last_delta_sync_at TIMESTAMPTZ,
		last_full_sync_at TIMESTAMPTZ,
		last_error VARCHAR)`,
	`CREATE TABLE owners (
		key VARCHAR PRIMARY KEY,
		title VARCHAR NOT NULL,
		avatar_url VARCHAR,
		parent_key VARCHAR)`,
	`CREATE TABLE http_cache (
		connector_id VARCHAR NOT NULL,
		url VARCHAR NOT NULL,
		etag VARCHAR NOT NULL,
		next_url VARCHAR)`,
	`CREATE TABLE attestation_sets (
		component VARCHAR NOT NULL,
		version VARCHAR NOT NULL,
		location_id INTEGER NOT NULL,
		checked_at TIMESTAMPTZ NOT NULL)`,
	`CREATE TABLE attestation_levels (
		component VARCHAR NOT NULL,
		version VARCHAR NOT NULL,
		location_id INTEGER NOT NULL,
		position INTEGER NOT NULL,
		resource_name VARCHAR,
		resource_version VARCHAR,
		extra_identity JSON,
		status VARCHAR NOT NULL,
		error VARCHAR)`,
	`CREATE TABLE attestation_entries (
		component VARCHAR NOT NULL,
		version VARCHAR NOT NULL,
		location_id INTEGER NOT NULL,
		level_position INTEGER NOT NULL,
		position INTEGER NOT NULL,
		kind VARCHAR NOT NULL,
		format VARCHAR,
		type VARCHAR,
		source VARCHAR NOT NULL,
		platform VARCHAR,
		signed VARCHAR NOT NULL,
		digest VARCHAR,
		resource_name VARCHAR,
		resource_version VARCHAR,
		resource_extra_identity JSON,
		url VARCHAR,
		build_level INTEGER,
		repository VARCHAR,
		builder VARCHAR,
		label VARCHAR,
		summary JSON)`,
	`CREATE TABLE version_changelogs (
		component VARCHAR NOT NULL,
		version VARCHAR NOT NULL,
		has_changelog BOOLEAN,
		error VARCHAR,
		checked_at TIMESTAMPTZ NOT NULL,
		PRIMARY KEY (component, version))`,
	fmt.Sprintf(`INSERT INTO schema_version VALUES (%d)`, SchemaVersion),
}

// checkSchema returns ErrSchemaMismatch unless db carries SchemaVersion.
// fresh reports a database without any tables.
func checkSchema(db *sql.DB) (fresh bool, err error) {
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM duckdb_tables()`).Scan(&tables); err != nil {
		return false, err
	}
	if tables == 0 {
		return true, nil
	}
	var v int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&v); err != nil {
		return false, fmt.Errorf("%w: %v", ErrSchemaMismatch, err)
	}
	if v != SchemaVersion {
		return false, fmt.Errorf("%w: file has %d, code has %d", ErrSchemaMismatch, v, SchemaVersion)
	}
	return false, nil
}
