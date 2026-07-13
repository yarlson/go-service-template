package api

import _ "embed"

// OpenAPI is the canonical HTTP contract.
//
//go:embed openapi.yaml
var OpenAPI []byte

// AsyncAPI is the canonical asynchronous message contract.
//
//go:embed asyncapi.yaml
var AsyncAPI []byte
