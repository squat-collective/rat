module github.com/rat-data/rat-plugin-pg-sync

go 1.24.0

toolchain go1.24.13

require (
	connectrpc.com/connect v1.19.1
	github.com/google/uuid v1.6.0
	github.com/rat-data/rat/platform v0.0.0
	github.com/rat-data/rat/sdk-go v0.0.0
)

require (
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/rat-data/rat/platform => /platform

replace github.com/rat-data/rat/sdk-go => /sdk-go
