package plugins

// Legacy loader — kept for backward compatibility during the transition to the
// open plugin registry. New code should use Manager and Registry directly.
//
// The old loader.go contained the slot-based Registry with typed plugin fields
// (auth, executor, sharing, enforcement, cloud) and a Load() function that
// connected to all configured plugins via switch/case. This has been replaced by:
//   - registry.go: open Registry with dynamic capability indexes
//   - manager.go: Manager for lifecycle orchestration (register, describe, enable/disable)
//
// This file is intentionally minimal. It will be deleted once all callers are
// migrated to the new Manager/Registry.

// Known plugin names that ratd understands.
// Kept for backward compatibility — new code uses Cap* constants from registry.go.
const (
	PluginAuth        = "auth"
	PluginExecutor    = "executor"
	PluginSharing     = "sharing"
	PluginEnforcement = "enforcement"
	PluginCloud       = "cloud"
)
