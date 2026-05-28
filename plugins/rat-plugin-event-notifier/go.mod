module github.com/rat-data/rat/examples/rat-plugin-event-notifier

go 1.24.0

require (
	connectrpc.com/connect v1.19.1
	github.com/rat-data/rat/platform v0.0.0
	github.com/rat-data/rat/sdk-go v0.0.0
)

require (
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// The platform module provides the generated plugin/v1 protobuf + ConnectRPC
// stubs. It is a sibling directory in the monorepo.
replace github.com/rat-data/rat/platform => /platform

replace github.com/rat-data/rat/sdk-go => /sdk-go
