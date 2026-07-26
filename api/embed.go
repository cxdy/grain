// Package api embeds the OpenAPI 3.0 specification for the grain daemon.
//
// Import github.com/cxdy/grain/api for the raw YAML bytes, or fetch
// GET /openapi.yaml from a running daemon.
package api

import _ "embed"

// OpenAPIYAML is the OpenAPI 3.0 specification (YAML).
//
//go:embed openapi.yaml
var OpenAPIYAML []byte
