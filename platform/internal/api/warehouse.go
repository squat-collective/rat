package api

import "context"

// WarehouseClient is ratd's handle to the active warehouse plugin (ADR-024).
//
// The warehouse owns the storage substrate (Iceberg+Nessie in the reference
// impl) and speaks warehouse/v1 over ConnectRPC. ratd holds this client to
// introspect the warehouse and — in later slices — to vend catalog / history /
// diff operations to consumers (the runner calls the warehouse directly).
//
// Defined as an interface here (like QueryStore) so the api package doesn't
// import the concrete client package; main.go injects the implementation.
type WarehouseClient interface {
	// Describe returns the warehouse's identity and advertised capability set.
	Describe(ctx context.Context) (WarehouseInfo, error)
}

// WarehouseInfo is the warehouse's self-description (from warehouse/v1 Describe).
type WarehouseInfo struct {
	Name         string
	Version      string
	Capabilities []string // e.g. "CAPABILITY_BRANCHING", "CAPABILITY_TIME_TRAVEL"
}
