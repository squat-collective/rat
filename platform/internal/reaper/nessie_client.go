// Package reaper provides the background data retention cleanup daemon.
package reaper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NessieClient provides a lightweight Go HTTP client for Nessie v2 branch operations.
type NessieClient interface {
	ListBranches(ctx context.Context) ([]NessieBranch, error)
	DeleteBranch(ctx context.Context, name, hash string) error
}

// NessieBranch represents a Nessie branch reference.
type NessieBranch struct {
	Name string
	Hash string
}

// HTTPNessieClient is a real Nessie v2 REST client.
type HTTPNessieClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPNessieClient creates a Nessie v2 client from a base URL (e.g., "http://nessie:19120").
func NewHTTPNessieClient(baseURL string) *HTTPNessieClient {
	base := strings.TrimRight(baseURL, "/")
	// Strip known suffixes to get the bare Nessie host
	for _, suffix := range []string{"/api/v1", "/api/v2", "/iceberg"} {
		if strings.HasSuffix(base, suffix) {
			base = base[:len(base)-len(suffix)]
			break
		}
	}
	return &HTTPNessieClient{
		baseURL:    base,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// nessieTreesResponse is the Nessie v2 GET /api/v2/trees response (simplified).
type nessieTreesResponse struct {
	References []nessieRef `json:"references"`
}

type nessieRef struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
	Type string `json:"type"` // "BRANCH" or "TAG"
}

// ListBranches returns all branches from Nessie.
func (c *HTTPNessieClient) ListBranches(ctx context.Context) ([]NessieBranch, error) {
	url := c.baseURL + "/api/v2/trees"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list branches: unexpected status %d", resp.StatusCode)
	}

	var treesResp nessieTreesResponse
	if err := json.NewDecoder(resp.Body).Decode(&treesResp); err != nil {
		return nil, fmt.Errorf("decode branches: %w", err)
	}

	var branches []NessieBranch
	for _, ref := range treesResp.References {
		if ref.Type == "BRANCH" {
			branches = append(branches, NessieBranch{Name: ref.Name, Hash: ref.Hash})
		}
	}
	return branches, nil
}

// DeleteBranch deletes a Nessie branch by name.
func (c *HTTPNessieClient) DeleteBranch(ctx context.Context, name, _ string) error {
	url := fmt.Sprintf("%s/api/v2/trees/%s", c.baseURL, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete branch %s: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete branch %s: unexpected status %d", name, resp.StatusCode)
	}
	return nil
}
