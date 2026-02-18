package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
)

// QueryColumn represents a column in a query result or table schema.
type QueryColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// QueryResult represents the result of an interactive SQL query.
type QueryResult struct {
	Columns    []QueryColumn          `json:"columns"`
	Rows       []map[string]interface{} `json:"rows"`
	TotalRows  int                    `json:"total_rows"`
	DurationMs int64                  `json:"duration_ms"`
}

// TableInfo represents a registered Iceberg table.
type TableInfo struct {
	Namespace   string  `json:"namespace"`
	Layer       string  `json:"layer"`
	Name        string  `json:"name"`
	RowCount    int64   `json:"row_count"`
	SizeBytes   int64   `json:"size_bytes"`
	Description string  `json:"description,omitempty"`
}

// TableDetail represents detailed table information including schema.
type TableDetail struct {
	TableInfo
	Columns []QueryColumn `json:"columns"`
	Owner   *string       `json:"owner,omitempty"`
}

// QueryStore defines the interface for interactive query operations.
// In production, this proxies to ratq (DuckDB sidecar) via gRPC.
type QueryStore interface {
	ExecuteQuery(ctx context.Context, sql string, namespace string, limit int) (*QueryResult, error)
	ListTables(ctx context.Context, namespace, layer string) ([]TableInfo, error)
	GetTable(ctx context.Context, namespace, layer, name string) (*TableDetail, error)
	PreviewTable(ctx context.Context, namespace, layer, name string, limit int) (*QueryResult, error)

	// GetBulkTableSchemas returns all tables with their column schemas in a single call,
	// avoiding N+1 gRPC calls when loading the full schema catalog.
	// Implementations should fetch all schemas in one round-trip where possible.
	GetBulkTableSchemas(ctx context.Context) ([]SchemaEntry, error)
}

// TableMetadataStore defines the persistence interface for table metadata (descriptions, ownership).
type TableMetadataStore interface {
	Get(ctx context.Context, namespace, layer, name string) (*domain.TableMetadata, error)
	Upsert(ctx context.Context, m *domain.TableMetadata) error
	ListAll(ctx context.Context) ([]domain.TableMetadata, error)
}

// UpdateTableMetadataRequest is the JSON body for PUT /api/v1/tables/{namespace}/{layer}/{name}/metadata.
type UpdateTableMetadataRequest struct {
	Description        *string           `json:"description,omitempty"`
	Owner              *string           `json:"owner,omitempty"`
	ColumnDescriptions map[string]string `json:"column_descriptions,omitempty"`
}

// SchemaEntry represents a table with its columns for the bulk schema endpoint.
type SchemaEntry struct {
	Namespace string        `json:"namespace"`
	Layer     string        `json:"layer"`
	Name      string        `json:"name"`
	Columns   []QueryColumn `json:"columns"`
}

// maxQueryLength is the maximum allowed SQL query length in bytes (100KB).
// Enforced at the API gateway before forwarding to ratq.
const maxQueryLength = 100_000

// ExecuteQueryRequest is the JSON body for POST /api/v1/query.
type ExecuteQueryRequest struct {
	SQL       string `json:"sql"`
	Namespace string `json:"namespace"`
	Limit     int    `json:"limit"`
}

// MountQueryRoutes registers query endpoints on the router.
func MountQueryRoutes(r chi.Router, srv *Server) {
	r.Post("/query", srv.HandleExecuteQuery)
	r.Get("/schema", srv.HandleGetSchema)
	r.Get("/tables", srv.HandleListTables)
	r.Get("/tables/{namespace}/{layer}/{name}", srv.HandleGetTable)
	r.Get("/tables/{namespace}/{layer}/{name}/preview", srv.HandlePreviewTable)
	if srv.TableMetadata != nil {
		r.Put("/tables/{namespace}/{layer}/{name}/metadata", srv.HandleUpdateTableMetadata)
	}
}

// HandleExecuteQuery runs an interactive SQL query via ratq.
func (s *Server) HandleExecuteQuery(w http.ResponseWriter, r *http.Request) {
	var req ExecuteQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if req.SQL == "" {
		errorJSON(w, "sql is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if len(req.SQL) > maxQueryLength {
		errorJSON(w, fmt.Sprintf("query too long (%d chars, max %d)", len(req.SQL), maxQueryLength), "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.Limit <= 0 {
		req.Limit = 1000
	}

	result, err := s.Query.ExecuteQuery(r.Context(), req.SQL, req.Namespace, req.Limit)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// HandleListTables returns all tables, optionally filtered, enriched with metadata descriptions.
func (s *Server) HandleListTables(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	layer := r.URL.Query().Get("layer")

	tables, err := s.Query.ListTables(r.Context(), namespace, layer)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if tables == nil {
		tables = []TableInfo{}
	}

	// Enrich with metadata descriptions if available.
	if s.TableMetadata != nil {
		allMeta, err := s.TableMetadata.ListAll(r.Context())
		if err == nil {
			metaByKey := make(map[string]domain.TableMetadata, len(allMeta))
			for _, m := range allMeta {
				metaByKey[m.Namespace+"/"+m.Layer+"/"+m.Name] = m
			}
			for i := range tables {
				key := tables[i].Namespace + "/" + tables[i].Layer + "/" + tables[i].Name
				if m, ok := metaByKey[key]; ok {
					tables[i].Description = m.Description
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tables": tables,
		"total":  len(tables),
	})
}

// HandleGetSchema returns all tables with their columns in a single call.
// Uses GetBulkTableSchemas to fetch all schemas in one round-trip instead of
// N+1 individual GetTable gRPC calls.
func (s *Server) HandleGetSchema(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Query.GetBulkTableSchemas(r.Context())
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if entries == nil {
		entries = []SchemaEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tables": entries,
	})
}

// HandleGetTable returns table schema and stats, enriched with metadata.
func (s *Server) HandleGetTable(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	table, err := s.Query.GetTable(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if table == nil {
		errorJSON(w, "table not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Enrich with metadata (description, owner, column descriptions).
	if s.TableMetadata != nil {
		meta, err := s.TableMetadata.Get(r.Context(), namespace, layer, name)
		if err == nil && meta != nil {
			table.Description = meta.Description
			table.Owner = meta.Owner
			for i := range table.Columns {
				if desc, ok := meta.ColumnDescriptions[table.Columns[i].Name]; ok {
					table.Columns[i].Description = desc
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, table)
}

// HandleUpdateTableMetadata creates or updates table metadata (description, owner, column descriptions).
func (s *Server) HandleUpdateTableMetadata(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	var req UpdateTableMetadataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Load existing or create new.
	meta, err := s.TableMetadata.Get(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if meta == nil {
		meta = &domain.TableMetadata{
			Namespace:          namespace,
			Layer:              layer,
			Name:               name,
			ColumnDescriptions: map[string]string{},
		}
	}

	if req.Description != nil {
		meta.Description = *req.Description
	}
	if req.Owner != nil {
		meta.Owner = req.Owner
	}
	if req.ColumnDescriptions != nil {
		meta.ColumnDescriptions = req.ColumnDescriptions
	}

	if err := s.TableMetadata.Upsert(r.Context(), meta); err != nil {
		internalError(w, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, meta)
}

// defaultPreviewTableLimit is the default number of rows returned by table preview.
const defaultPreviewTableLimit = 50

// maxPreviewTableLimit is the maximum allowed preview limit.
const maxPreviewTableLimit = 1000

// HandlePreviewTable returns the first N rows of a table.
// The limit can be set via ?limit= query parameter (default 50, max 1000).
func (s *Server) HandlePreviewTable(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	limit := defaultPreviewTableLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxPreviewTableLimit {
		limit = maxPreviewTableLimit
	}

	result, err := s.Query.PreviewTable(r.Context(), namespace, layer, name, limit)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
