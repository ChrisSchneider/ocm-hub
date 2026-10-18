// Package gen holds types and a client generated from api/openapi.yaml.
// Do not edit gen.go; run `go generate ./internal/rest/gen/`.
package gen

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../../api/openapi.yaml
