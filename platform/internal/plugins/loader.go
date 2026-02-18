package plugins

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	authv1 "github.com/rat-data/rat/platform/gen/auth/v1"
	"github.com/rat-data/rat/platform/gen/auth/v1/authv1connect"
	cloudv1 "github.com/rat-data/rat/platform/gen/cloud/v1"
	"github.com/rat-data/rat/platform/gen/cloud/v1/cloudv1connect"
	enforcementv1 "github.com/rat-data/rat/platform/gen/enforcement/v1"
	"github.com/rat-data/rat/platform/gen/enforcement/v1/enforcementv1connect"
	"github.com/rat-data/rat/platform/gen/executor/v1/executorv1connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
	sharingv1 "github.com/rat-data/rat/platform/gen/sharing/v1"
	"github.com/rat-data/rat/platform/gen/sharing/v1/sharingv1connect"
	"github.com/rat-data/rat/platform/internal/config"
	"github.com/rat-data/rat/platform/internal/domain"
)

const healthCheckTimeout = 5 * time.Second

// Known plugin names that ratd understands.
const (
	PluginAuth        = "auth"
	PluginExecutor    = "executor"
	PluginSharing     = "sharing"
	PluginEnforcement = "enforcement"
	PluginCloud       = "cloud"
)

// Registry holds connected and healthy plugin clients.
// An empty Registry represents the community edition (no plugins).
type Registry struct {
	edition     string
	auth        *authPlugin
	executor    *executorPlugin
	sharing     *sharingPlugin
	enforcement *enforcementPlugin
	cloud       *cloudPlugin
}

// sharingPlugin wraps the sharing ConnectRPC client.
type sharingPlugin struct {
	client sharingv1connect.SharingServiceClient
}

// enforcementPlugin wraps the enforcement ConnectRPC client.
type enforcementPlugin struct {
	client enforcementv1connect.EnforcementServiceClient
}

// authPlugin wraps the auth ConnectRPC client.
type authPlugin struct {
	client authv1connect.AuthServiceClient
}

// executorPlugin wraps the executor ConnectRPC client.
type executorPlugin struct {
	client executorv1connect.ExecutorServiceClient
	addr   string
}

// cloudPlugin wraps the cloud ConnectRPC client.
type cloudPlugin struct {
	client cloudv1connect.CloudServiceClient
}

// healthChecker is the interface for plugin health checks.
// Every plugin container must implement PluginService.HealthCheck.
type healthChecker interface {
	HealthCheck(context.Context, *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error)
}

// pluginClientFactory creates ConnectRPC clients for testing.
type pluginClientFactory struct {
	newPluginClient      func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) pluginv1connect.PluginServiceClient
	newAuthClient        func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) authv1connect.AuthServiceClient
	newExecutorClient    func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) executorv1connect.ExecutorServiceClient
	newSharingClient     func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) sharingv1connect.SharingServiceClient
	newEnforcementClient func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) enforcementv1connect.EnforcementServiceClient
	newCloudClient       func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) cloudv1connect.CloudServiceClient
}

// defaultFactory creates real ConnectRPC clients.
func defaultFactory() *pluginClientFactory {
	return &pluginClientFactory{
		newPluginClient:      pluginv1connect.NewPluginServiceClient,
		newAuthClient:        authv1connect.NewAuthServiceClient,
		newExecutorClient:    executorv1connect.NewExecutorServiceClient,
		newSharingClient:     sharingv1connect.NewSharingServiceClient,
		newEnforcementClient: enforcementv1connect.NewEnforcementServiceClient,
		newCloudClient:       cloudv1connect.NewCloudServiceClient,
	}
}

// Load connects to all configured plugins, health-checks them, and returns a Registry.
// Unhealthy plugins are logged as warnings and disabled.
// Unknown plugin names are skipped with a warning.
// Pass an optional HTTP client for TLS transport; defaults to http.DefaultClient.
func Load(ctx context.Context, cfg *config.Config, httpClient ...*http.Client) (*Registry, error) {
	var c *http.Client
	if len(httpClient) > 0 && httpClient[0] != nil {
		c = httpClient[0]
	}
	if c == nil {
		c = http.DefaultClient
	}
	return loadWithFactory(ctx, cfg, defaultFactory(), c)
}

// loadWithFactory is the internal implementation that accepts a client factory for testing.
func loadWithFactory(ctx context.Context, cfg *config.Config, factory *pluginClientFactory, httpClient *http.Client) (*Registry, error) {
	reg := &Registry{
		edition: cfg.Edition,
	}

	for name, pluginCfg := range cfg.Plugins {
		switch name {
		case PluginAuth:
			if err := reg.loadAuth(ctx, pluginCfg, factory, httpClient); err != nil {
				slog.Warn("auth plugin unhealthy, disabled", "addr", pluginCfg.Addr, "error", err)
			}
		case PluginExecutor:
			if err := reg.loadExecutor(ctx, pluginCfg, factory, httpClient); err != nil {
				slog.Warn("executor plugin unhealthy, disabled", "addr", pluginCfg.Addr, "error", err)
			}
		case PluginSharing:
			if err := reg.loadSharing(ctx, pluginCfg, factory, httpClient); err != nil {
				slog.Warn("sharing plugin unhealthy, disabled", "addr", pluginCfg.Addr, "error", err)
			}
		case PluginEnforcement:
			if err := reg.loadEnforcement(ctx, pluginCfg, factory, httpClient); err != nil {
				slog.Warn("enforcement plugin unhealthy, disabled", "addr", pluginCfg.Addr, "error", err)
			}
		case PluginCloud:
			if err := reg.loadCloud(ctx, pluginCfg, factory, httpClient); err != nil {
				slog.Warn("cloud plugin unhealthy, disabled", "addr", pluginCfg.Addr, "error", err)
			}
		default:
			slog.Warn("unknown plugin, skipped", "name", name)
		}
	}

	return reg, nil
}

// loadAuth connects to the auth plugin, health-checks it, and stores the client.
func (r *Registry) loadAuth(ctx context.Context, cfg config.PluginConfig, factory *pluginClientFactory, httpClient *http.Client) error {
	addr := ensureScheme(cfg.Addr)

	// Health check first
	healthClient := factory.newPluginClient(httpClient, addr)
	healthMsg, err := checkHealth(ctx, healthClient)
	if err != nil {
		return err
	}

	// Version negotiation — warn on mismatches but don't block loading.
	if err := CheckVersionFromHealthMessage(PluginAuth, healthMsg); err != nil {
		slog.Warn("auth plugin version negotiation issue", "error", err)
	}

	// Healthy — create the auth client
	authClient := factory.newAuthClient(httpClient, addr)
	r.auth = &authPlugin{client: authClient}

	slog.Info("auth plugin loaded", "addr", cfg.Addr)
	return nil
}

// checkHealth calls PluginService.HealthCheck with a timeout.
// Returns the health check response message (used for version negotiation).
func checkHealth(ctx context.Context, client healthChecker) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	resp, err := client.HealthCheck(ctx, connect.NewRequest(&pluginv1.HealthCheckRequest{}))
	if err != nil {
		return "", fmt.Errorf("health check failed: %w", err)
	}

	if resp.Msg.Status != pluginv1.Status_STATUS_SERVING {
		return "", fmt.Errorf("plugin not serving: %s", resp.Msg.Message)
	}

	return resp.Msg.Message, nil
}

// Features returns the dynamic feature set based on loaded plugins.
// This replaces the hardcoded features in health.go.
func (r *Registry) Features() domain.Features {
	executorType := "warmpool"
	if r.executor != nil {
		executorType = "container"
	}
	features := domain.Features{
		Edition:    r.edition,
		Namespaces: r.edition != "community",
		MultiUser:  r.auth != nil,
		Plugins: map[string]domain.PluginFeature{
			"auth":        {Enabled: r.auth != nil},
			"sharing":     {Enabled: r.sharing != nil},
			"executor":    {Enabled: true, Type: executorType},
			"audit":       {Enabled: false},
			"enforcement": {Enabled: r.enforcement != nil},
			"cloud":       {Enabled: r.cloud != nil},
		},
	}
	return features
}

// AuthEnabled returns true if the auth plugin is loaded and healthy.
func (r *Registry) AuthEnabled() bool {
	return r.auth != nil
}

// Authenticate delegates to the auth plugin's Authenticate RPC.
// Only call this if AuthEnabled() returns true.
func (r *Registry) Authenticate(ctx context.Context, token string) (*authv1.AuthenticateResponse, error) {
	if r.auth == nil {
		return nil, fmt.Errorf("auth plugin not loaded")
	}

	resp, err := r.auth.client.Authenticate(ctx, connect.NewRequest(&authv1.AuthenticateRequest{
		Token: token,
	}))
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	return resp.Msg, nil
}

// loadExecutor connects to the executor plugin, health-checks it, and stores the client.
func (r *Registry) loadExecutor(ctx context.Context, cfg config.PluginConfig, factory *pluginClientFactory, httpClient *http.Client) error {
	addr := ensureScheme(cfg.Addr)

	// Health check first
	healthClient := factory.newPluginClient(httpClient, addr)
	healthMsg, err := checkHealth(ctx, healthClient)
	if err != nil {
		return err
	}

	if err := CheckVersionFromHealthMessage(PluginExecutor, healthMsg); err != nil {
		slog.Warn("executor plugin version negotiation issue", "error", err)
	}

	// Healthy — create the executor client
	execClient := factory.newExecutorClient(httpClient, addr)
	r.executor = &executorPlugin{client: execClient, addr: addr}

	slog.Info("executor plugin loaded", "addr", cfg.Addr)
	return nil
}

// ExecutorEnabled returns true if the executor plugin is loaded and healthy.
func (r *Registry) ExecutorEnabled() bool {
	return r.executor != nil
}

// GetExecutorAddr returns the executor plugin's address for creating a PluginExecutor.
func (r *Registry) GetExecutorAddr() string {
	if r.executor == nil {
		return ""
	}
	return r.executor.addr
}

// loadSharing connects to the sharing plugin, health-checks it, and stores the client.
func (r *Registry) loadSharing(ctx context.Context, cfg config.PluginConfig, factory *pluginClientFactory, httpClient *http.Client) error {
	addr := ensureScheme(cfg.Addr)

	healthClient := factory.newPluginClient(httpClient, addr)
	healthMsg, err := checkHealth(ctx, healthClient)
	if err != nil {
		return err
	}

	if err := CheckVersionFromHealthMessage(PluginSharing, healthMsg); err != nil {
		slog.Warn("sharing plugin version negotiation issue", "error", err)
	}

	sharingClient := factory.newSharingClient(httpClient, addr)
	r.sharing = &sharingPlugin{client: sharingClient}

	slog.Info("sharing plugin loaded", "addr", cfg.Addr)
	return nil
}

// loadEnforcement connects to the enforcement plugin, health-checks it, and stores the client.
func (r *Registry) loadEnforcement(ctx context.Context, cfg config.PluginConfig, factory *pluginClientFactory, httpClient *http.Client) error {
	addr := ensureScheme(cfg.Addr)

	healthClient := factory.newPluginClient(httpClient, addr)
	healthMsg, err := checkHealth(ctx, healthClient)
	if err != nil {
		return err
	}

	if err := CheckVersionFromHealthMessage(PluginEnforcement, healthMsg); err != nil {
		slog.Warn("enforcement plugin version negotiation issue", "error", err)
	}

	enfClient := factory.newEnforcementClient(httpClient, addr)
	r.enforcement = &enforcementPlugin{client: enfClient}

	slog.Info("enforcement plugin loaded", "addr", cfg.Addr)
	return nil
}

// SharingEnabled returns true if the sharing plugin is loaded and healthy.
func (r *Registry) SharingEnabled() bool {
	return r.sharing != nil
}

// EnforcementEnabled returns true if the enforcement plugin is loaded and healthy.
func (r *Registry) EnforcementEnabled() bool {
	return r.enforcement != nil
}

// ShareResource delegates to the sharing plugin's ShareResource RPC.
func (r *Registry) ShareResource(ctx context.Context, grantorID, granteeID, resourceType, resourceID, permission string) (*sharingv1.ShareResourceResponse, error) {
	if r.sharing == nil {
		return nil, fmt.Errorf("sharing plugin not loaded")
	}

	permEnum := sharingv1.Permission_PERMISSION_READ
	switch permission {
	case "write":
		permEnum = sharingv1.Permission_PERMISSION_WRITE
	case "admin":
		permEnum = sharingv1.Permission_PERMISSION_ADMIN
	}

	resp, err := r.sharing.client.ShareResource(ctx, connect.NewRequest(&sharingv1.ShareResourceRequest{
		GrantorId:    grantorID,
		GranteeId:    granteeID,
		ResourceType: resourceType,
		ResourceId:   resourceID,
		Permission:   permEnum,
	}))
	if err != nil {
		return nil, fmt.Errorf("share resource: %w", err)
	}
	return resp.Msg, nil
}

// RevokeAccess delegates to the sharing plugin's RevokeAccess RPC.
func (r *Registry) RevokeAccess(ctx context.Context, grantID, revokedBy string) error {
	if r.sharing == nil {
		return fmt.Errorf("sharing plugin not loaded")
	}

	_, err := r.sharing.client.RevokeAccess(ctx, connect.NewRequest(&sharingv1.RevokeAccessRequest{
		GrantId:   grantID,
		RevokedBy: revokedBy,
	}))
	if err != nil {
		return fmt.Errorf("revoke access: %w", err)
	}
	return nil
}

// ListAccess delegates to the sharing plugin's ListAccess RPC.
func (r *Registry) ListAccess(ctx context.Context, resourceType, resourceID string) (*sharingv1.ListAccessResponse, error) {
	if r.sharing == nil {
		return nil, fmt.Errorf("sharing plugin not loaded")
	}

	resp, err := r.sharing.client.ListAccess(ctx, connect.NewRequest(&sharingv1.ListAccessRequest{
		ResourceType: resourceType,
		ResourceId:   resourceID,
	}))
	if err != nil {
		return nil, fmt.Errorf("list access: %w", err)
	}
	return resp.Msg, nil
}

// CanAccess delegates to the enforcement plugin's CanAccess RPC.
func (r *Registry) CanAccess(ctx context.Context, userID, resourceType, resourceID, action string) (bool, error) {
	if r.enforcement == nil {
		return false, fmt.Errorf("enforcement plugin not loaded")
	}

	resp, err := r.enforcement.client.CanAccess(ctx, connect.NewRequest(&enforcementv1.CanAccessRequest{
		UserId:       userID,
		ResourceType: resourceType,
		ResourceId:   resourceID,
		Action:       action,
	}))
	if err != nil {
		return false, fmt.Errorf("can access: %w", err)
	}
	return resp.Msg.Allowed, nil
}

// loadCloud connects to the cloud plugin, health-checks it, and stores the client.
func (r *Registry) loadCloud(ctx context.Context, cfg config.PluginConfig, factory *pluginClientFactory, httpClient *http.Client) error {
	addr := ensureScheme(cfg.Addr)

	healthClient := factory.newPluginClient(httpClient, addr)
	healthMsg, err := checkHealth(ctx, healthClient)
	if err != nil {
		return err
	}

	if err := CheckVersionFromHealthMessage(PluginCloud, healthMsg); err != nil {
		slog.Warn("cloud plugin version negotiation issue", "error", err)
	}

	cloudClient := factory.newCloudClient(httpClient, addr)
	r.cloud = &cloudPlugin{client: cloudClient}

	slog.Info("cloud plugin loaded", "addr", cfg.Addr)
	return nil
}

// CloudEnabled returns true if the cloud plugin is loaded and healthy.
func (r *Registry) CloudEnabled() bool {
	return r.cloud != nil
}

// GetCredentials delegates to the cloud plugin's GetCredentials RPC.
func (r *Registry) GetCredentials(ctx context.Context, userID, namespace string) (*cloudv1.GetCredentialsResponse, error) {
	if r.cloud == nil {
		return nil, fmt.Errorf("cloud plugin not loaded")
	}

	resp, err := r.cloud.client.GetCredentials(ctx, connect.NewRequest(&cloudv1.GetCredentialsRequest{
		UserId:    userID,
		Namespace: namespace,
	}))
	if err != nil {
		return nil, fmt.Errorf("get credentials: %w", err)
	}

	return resp.Msg, nil
}

// ensureScheme adds http:// if the address has no scheme.
func ensureScheme(addr string) string {
	if addr != "" && !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		return "http://" + addr
	}
	return addr
}
