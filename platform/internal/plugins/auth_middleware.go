package plugins

import (
	"encoding/json"
	"net/http"
	"strings"
)

// extractBearerToken extracts the token from "Authorization: Bearer <token>".
func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Error is intentionally ignored: headers and status code are already
	// written, so there is nothing useful we can do if encoding fails.
	_ = json.NewEncoder(w).Encode(v)
}
