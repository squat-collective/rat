module github.com/rat-data/rat/examples/rat-plugin-dev-assistant

go 1.26.0

require (
	connectrpc.com/connect v1.20.0
	github.com/rat-data/rat/platform v0.0.0
	github.com/rat-data/rat/sdk-go v0.0.0
)

require (
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// The platform module provides the generated plugin/v1 ConnectRPC stubs.
replace github.com/rat-data/rat/platform => /platform

replace github.com/rat-data/rat/sdk-go => /sdk-go
