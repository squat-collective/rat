package sdk

import (
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
)

// DescribeBuilder builds a *pluginv1.DescribeResponse fluently. Cuts
// down the ~30-line Describe handler in each plugin to roughly 10 lines
// while still surfacing every field the proto defines.
//
// Typical usage:
//
//	return sdk.NewDescribe("secrets", "0.1.0", "encrypted KV store").
//	    WithRoute("GET", "/secrets", "list secrets").
//	    WithRoute("POST", "/secrets", "upsert").
//	    WithUI(bundleURL, bundleHash, navItems, routes).
//	    WithPlatformToken(token).
//	    WithConfigSchema(schemaJSON).
//	    Build()
type DescribeBuilder struct {
	resp *pluginv1.DescribeResponse
}

// NewDescribe seeds a builder with the three always-required fields
// (name, version, description). Everything else is opt-in.
func NewDescribe(name, version, description string) *DescribeBuilder {
	return &DescribeBuilder{
		resp: &pluginv1.DescribeResponse{
			Name:        name,
			Version:     version,
			Description: description,
		},
	}
}

// WithRoute appends a route declaration. Method + path + a one-line
// human description that surfaces in the portal's plugin browser.
func (b *DescribeBuilder) WithRoute(method, path, description string) *DescribeBuilder {
	b.resp.Routes = append(b.resp.Routes, &pluginv1.RouteDeclaration{
		Method:      method,
		Path:        path,
		Description: description,
	})
	return b
}

// WithUI attaches the portal-UI descriptor. bundleURL is the absolute
// URL the portal's <script> tag points at (typically
// http://<plugin-addr>/bundle.js), bundleHash is the SRI hash
// (use SRIHash on the embedded bundle), and navItems + routes
// describe how the portal should render the plugin.
//
// Pass nil for navItems or routes to omit them.
func (b *DescribeBuilder) WithUI(
	bundleURL string,
	bundleHash string,
	navItems []*pluginv1.UINavItem,
	routes []*pluginv1.UIRoute,
) *DescribeBuilder {
	b.resp.Ui = &pluginv1.PluginUIDescriptor{
		BundleUrl:  bundleURL,
		BundleHash: bundleHash,
		NavItems:   navItems,
		Routes:     routes,
	}
	return b
}

// WithPlatformToken sets the per-startup random token ratd reads from
// Describe and injects on every proxied request. Always pass
// sdk.RandomToken() generated once in main.
func (b *DescribeBuilder) WithPlatformToken(token string) *DescribeBuilder {
	b.resp.PlatformToken = token
	return b
}

// WithConfigSchema sets the JSON Schema for the plugin's config object.
// The portal renders this as a form on the plugin's config page.
func (b *DescribeBuilder) WithConfigSchema(schema string) *DescribeBuilder {
	b.resp.ConfigSchemaJson = schema
	return b
}

// WithCapabilities sets the well-known capability strings ("auth",
// "executor", "sharing", etc). Plugins that don't implement any of
// the typed capability protos can omit this.
func (b *DescribeBuilder) WithCapabilities(caps ...string) *DescribeBuilder {
	b.resp.Capabilities = caps
	return b
}

// WithEventSubscriptions sets the event channels the plugin subscribes
// to ("run_completed", "pipeline_created", …).
func (b *DescribeBuilder) WithEventSubscriptions(events ...string) *DescribeBuilder {
	b.resp.EventSubscriptions = events
	return b
}

// WithProvidesWorker marks the plugin as running background work.
// ratd uses this hint to size health-check timeouts.
func (b *DescribeBuilder) WithProvidesWorker(v bool) *DescribeBuilder {
	b.resp.ProvidesWorker = v
	return b
}

// Build returns the assembled DescribeResponse. The builder may be
// reused safely after Build since the response is a pointer to the
// internal value — but in practice every caller throws it away.
func (b *DescribeBuilder) Build() *pluginv1.DescribeResponse {
	return b.resp
}
