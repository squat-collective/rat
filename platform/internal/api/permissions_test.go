package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	permissionv1 "github.com/rat-data/rat/platform/gen/permission/v1"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockPermissionProvider implements both api.PluginRegistry and api.PermissionProvider.
type mockPermissionProvider struct {
	enabled bool
	verbs   []*permissionv1.VerbDefinition
	grants  []*permissionv1.Grant
	groups  []*permissionv1.GroupInfo
	members []*permissionv1.GroupMember
	access  []*permissionv1.EffectiveAccess
}

func (m *mockPermissionProvider) Features() domain.Features {
	return domain.Features{
		Edition: "pro",
		Plugins: map[string]domain.PluginFeature{
			"auth":       {Enabled: true},
			"permission": {Enabled: m.enabled},
		},
	}
}

func (m *mockPermissionProvider) PermissionEnabled() bool { return m.enabled }

func (m *mockPermissionProvider) ListVerbs(_ context.Context) (*permissionv1.ListVerbsResponse, error) {
	return &permissionv1.ListVerbsResponse{Verbs: m.verbs}, nil
}

func (m *mockPermissionProvider) RegisterVerb(_ context.Context, name string, implies []string, description string) error {
	m.verbs = append(m.verbs, &permissionv1.VerbDefinition{Name: name, Implies: implies, Description: description})
	return nil
}

func (m *mockPermissionProvider) ListGrants(_ context.Context, _ string, _ permissionv1.PrincipalType, _ string) (*permissionv1.ListGrantsResponse, error) {
	return &permissionv1.ListGrantsResponse{Grants: m.grants}, nil
}

func (m *mockPermissionProvider) CreatePermissionGrant(_ context.Context, req *permissionv1.CreateGrantRequest) (*permissionv1.CreateGrantResponse, error) {
	grant := &permissionv1.Grant{
		GrantId:       "g-123",
		PrincipalType: req.PrincipalType,
		PrincipalId:   req.PrincipalId,
		Resource:      req.Resource,
		Verb:          req.Verb,
		GrantedBy:     req.GrantedBy,
		CreatedAt:     timestamppb.Now(),
	}
	m.grants = append(m.grants, grant)
	return &permissionv1.CreateGrantResponse{Grant: grant}, nil
}

func (m *mockPermissionProvider) RevokePermissionGrant(_ context.Context, _ string) (*permissionv1.RevokeGrantResponse, error) {
	return &permissionv1.RevokeGrantResponse{Revoked: true}, nil
}

func (m *mockPermissionProvider) ListGroups(_ context.Context) (*permissionv1.ListGroupsResponse, error) {
	return &permissionv1.ListGroupsResponse{Groups: m.groups}, nil
}

func (m *mockPermissionProvider) CreatePermissionGroup(_ context.Context, name, description string) (*permissionv1.CreateGroupResponse, error) {
	group := &permissionv1.GroupInfo{GroupId: "grp-123", Name: name, Description: description, CreatedAt: timestamppb.Now()}
	m.groups = append(m.groups, group)
	return &permissionv1.CreateGroupResponse{Group: group}, nil
}

func (m *mockPermissionProvider) DeletePermissionGroup(_ context.Context, _ string) (*permissionv1.DeleteGroupResponse, error) {
	return &permissionv1.DeleteGroupResponse{Deleted: true}, nil
}

func (m *mockPermissionProvider) ListGroupMembers(_ context.Context, _ string) (*permissionv1.ListGroupMembersResponse, error) {
	return &permissionv1.ListGroupMembersResponse{Members: m.members}, nil
}

func (m *mockPermissionProvider) AddGroupMember(_ context.Context, _ string, _ permissionv1.PrincipalType, _ string) error {
	return nil
}

func (m *mockPermissionProvider) RemoveGroupMember(_ context.Context, _ string, _ permissionv1.PrincipalType, _ string) (*permissionv1.RemoveGroupMemberResponse, error) {
	return &permissionv1.RemoveGroupMemberResponse{Removed: true}, nil
}

func (m *mockPermissionProvider) CheckPermissionAccess(_ context.Context, _ string, _ []string, _ string, _ string) (*permissionv1.CheckAccessResponse, error) {
	return &permissionv1.CheckAccessResponse{Allowed: true}, nil
}

func (m *mockPermissionProvider) ListResourceAccess(_ context.Context, _ string) (*permissionv1.ListResourceAccessResponse, error) {
	return &permissionv1.ListResourceAccessResponse{Access: m.access}, nil
}

func (m *mockPermissionProvider) ListPrincipalAccess(_ context.Context, _ string, _ []string, _ string) (*permissionv1.ListPrincipalAccessResponse, error) {
	return &permissionv1.ListPrincipalAccessResponse{Access: m.access}, nil
}

func (m *mockPermissionProvider) RemovePermissionResource(_ context.Context, _ string, _ bool) (*permissionv1.RemoveResourceResponse, error) {
	return &permissionv1.RemoveResourceResponse{ResourcesRemoved: 1, GrantsRemoved: 3}, nil
}

// newPermissionTestRouter creates a chi router with permission routes and auth context.
func newPermissionTestRouter(pp *mockPermissionProvider) http.Handler {
	srv := &api.Server{
		Plugins: pp,
		Auth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := plugins.ContextWithUser(r.Context(), &domain.UserIdentity{
					UserID: "alice",
					Email:  "alice@rat.dev",
				})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	}
	return api.NewRouter(srv)
}

// ── Verbs ──────────────────────────────────────────────────────────────────

func TestListVerbs_ReturnsVerbs(t *testing.T) {
	pp := &mockPermissionProvider{
		enabled: true,
		verbs: []*permissionv1.VerbDefinition{
			{Name: "read", Implies: nil},
			{Name: "admin", Implies: []string{"read", "write"}},
		},
	}
	router := newPermissionTestRouter(pp)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions/verbs", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp permissionv1.ListVerbsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Len(t, resp.Verbs, 2)
}

func TestRegisterVerb_Success(t *testing.T) {
	pp := &mockPermissionProvider{enabled: true}
	router := newPermissionTestRouter(pp)

	body, _ := json.Marshal(map[string]any{"name": "deploy", "implies": []string{"read"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/verbs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Len(t, pp.verbs, 1)
}

// ── Grants ─────────────────────────────────────────────────────────────────

func TestListGrants_ReturnsGrants(t *testing.T) {
	pp := &mockPermissionProvider{
		enabled: true,
		grants: []*permissionv1.Grant{
			{GrantId: "g-1", PrincipalId: "bob", Resource: "gold/*", Verb: "read"},
		},
	}
	router := newPermissionTestRouter(pp)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions/grants?resource=gold/*", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCreateGrant_Success(t *testing.T) {
	pp := &mockPermissionProvider{enabled: true}
	router := newPermissionTestRouter(pp)

	body, _ := json.Marshal(map[string]any{
		"principal_type": "user",
		"principal_id":   "bob",
		"resource":       "gold/pipeline/bronze/orders",
		"verb":           "read",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/grants", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Len(t, pp.grants, 1)
}

func TestCreateGrant_MissingFields_ReturnsBadRequest(t *testing.T) {
	pp := &mockPermissionProvider{enabled: true}
	router := newPermissionTestRouter(pp)

	body, _ := json.Marshal(map[string]any{"principal_type": "user"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/grants", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── Groups ─────────────────────────────────────────────────────────────────

func TestListGroups_ReturnsGroups(t *testing.T) {
	pp := &mockPermissionProvider{
		enabled: true,
		groups: []*permissionv1.GroupInfo{
			{GroupId: "grp-1", Name: "data-eng"},
		},
	}
	router := newPermissionTestRouter(pp)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions/groups", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCreateGroup_Success(t *testing.T) {
	pp := &mockPermissionProvider{enabled: true}
	router := newPermissionTestRouter(pp)

	body, _ := json.Marshal(map[string]any{"name": "data-eng", "description": "Data engineering"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestCreateGroup_MissingName_ReturnsBadRequest(t *testing.T) {
	pp := &mockPermissionProvider{enabled: true}
	router := newPermissionTestRouter(pp)

	body, _ := json.Marshal(map[string]any{"description": "no name"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/grants", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── Plugin Not Loaded ──────────────────────────────────────────────────────

func TestPermissionEndpoints_PluginDisabled_Returns501(t *testing.T) {
	pp := &mockPermissionProvider{enabled: false}
	router := newPermissionTestRouter(pp)

	// Permission routes are mounted but handler returns 501 when plugin is disabled.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions/verbs", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

// ── Access Check ───────────────────────────────────────────────────────────

func TestCheckAccess_Success(t *testing.T) {
	pp := &mockPermissionProvider{enabled: true}
	router := newPermissionTestRouter(pp)

	body, _ := json.Marshal(map[string]any{
		"user_id":  "bob",
		"resource": "gold/pipeline/bronze/orders",
		"verb":     "read",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/check", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCheckAccess_MissingFields_ReturnsBadRequest(t *testing.T) {
	pp := &mockPermissionProvider{enabled: true}
	router := newPermissionTestRouter(pp)

	body, _ := json.Marshal(map[string]any{"user_id": "bob"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/permissions/check", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
