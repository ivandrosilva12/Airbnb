package openapi

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// MarshalJSON serialises the document to indented JSON. OpenAPI 3.0
// natively accepts JSON, so this is the most direct format for tooling
// (Stoplight, Postman, openapi-generator all prefer it).
func MarshalJSON(d Document) ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

// MarshalYAML serialises the document to YAML. Useful for humans and
// for tools that historically prefer YAML (Swagger UI, redoc-cli). The
// struct tags on spec.go drive both encoders.
func MarshalYAML(d Document) ([]byte, error) {
	return yaml.Marshal(d)
}
