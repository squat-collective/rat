module github.com/rat-data/rat/examples/rat-plugin-charts

go 1.24.0

require (
	connectrpc.com/connect v1.19.1
	github.com/rat-data/rat/platform v0.0.0
	golang.org/x/net v0.49.0
)

require (
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// The platform module provides the generated plugin/v1 ConnectRPC stubs.
replace github.com/rat-data/rat/platform => ../../platform
