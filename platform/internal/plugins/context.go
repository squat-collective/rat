// Package plugins provides the plugin loader, registry, and middleware for ratd.
// It connects to plugin containers via ConnectRPC and exposes their capabilities
// through the Registry. Community edition runs with an empty registry.
package plugins

import (
	"context"

	authv1 "github.com/rat-data/rat/platform/gen/auth/v1"
)

type contextKey struct{}

// ContextWithUser stores a UserIdentity in the request context.
func ContextWithUser(ctx context.Context, user *authv1.UserIdentity) context.Context {
	return context.WithValue(ctx, contextKey{}, user)
}

// UserFromContext extracts the UserIdentity from the request context.
// Returns nil if no user is present (community edition / unauthenticated).
func UserFromContext(ctx context.Context) *authv1.UserIdentity {
	user, _ := ctx.Value(contextKey{}).(*authv1.UserIdentity)
	return user
}
