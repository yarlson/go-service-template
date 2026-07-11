package api

import _ "embed"

// OpenAPI is the canonical service contract.
//
//go:embed openapi.yaml
var OpenAPI []byte
