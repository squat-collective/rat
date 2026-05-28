package plugins

import (
	"context"

	"github.com/rat-data/rat/platform/internal/domain"
)

// PipelineOwnerLookup provides pipeline ownership lookups for authorization.
// Defined here to avoid importing the api package (which imports plugins).
type PipelineOwnerLookup interface {
	GetPipelineByID(ctx context.Context, id string) (*domain.Pipeline, error)
}

// PluginAuthorizer checks ownership locally (Postgres), then delegates
// to the enforcement plugin for sharing grants.
type PluginAuthorizer struct {
	registry  *Registry
	pipelines PipelineOwnerLookup
}

// NewPluginAuthorizer creates a PluginAuthorizer that checks ownership
// in Postgres first, then falls back to the enforcement plugin.
func NewPluginAuthorizer(registry *Registry, pipelines PipelineOwnerLookup) *PluginAuthorizer {
	return &PluginAuthorizer{
		registry:  registry,
		pipelines: pipelines,
	}
}

func (a *PluginAuthorizer) CanAccess(ctx context.Context, userID, resourceType, resourceID, action string) (bool, error) {
	// 1. Check ownership locally (owner has full access).
	if resourceType == "pipeline" {
		pipeline, err := a.pipelines.GetPipelineByID(ctx, resourceID)
		if err == nil && pipeline != nil && pipeline.Owner != nil && *pipeline.Owner == userID {
			return true, nil
		}
	}

	// 2. Delegate to enforcement plugin for sharing grants.
	if a.registry.EnforcementEnabled() {
		return a.registry.CanAccess(ctx, userID, resourceType, resourceID, action)
	}

	// No enforcement plugin = deny non-owners.
	return false, nil
}

// Filter returns the subset of resourceIDs the user can access. Default
// implementation loops CanAccess per ID — fine for the typical Pro page
// size (≤100 items). When the enforcement plugin grows a batch RPC we
// can swap this for one round-trip.
func (a *PluginAuthorizer) Filter(ctx context.Context, userID, resourceType, action string, resourceIDs []string) ([]string, error) {
	if len(resourceIDs) == 0 {
		return resourceIDs, nil
	}
	allowed := make([]string, 0, len(resourceIDs))
	for _, id := range resourceIDs {
		ok, err := a.CanAccess(ctx, userID, resourceType, id, action)
		if err != nil {
			return nil, err
		}
		if ok {
			allowed = append(allowed, id)
		}
	}
	return allowed, nil
}
