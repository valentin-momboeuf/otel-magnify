package api

import (
	"net/http"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

type authMethodsResponse struct {
	Methods []ext.AuthMethod `json:"methods"`
}

func (a *API) handleListAuthMethods(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, authMethodsResponse{Methods: a.authMethods()})
}
