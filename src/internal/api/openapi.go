package api

import (
	_ "embed"
	"net/http"
)

// openapiDoc describes every route in Handler. TestOpenAPIMatchesTheRoutes fails when the two disagree.
//
//go:embed openapi.json
var openapiDoc []byte

func (s *Server) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(openapiDoc)
}
