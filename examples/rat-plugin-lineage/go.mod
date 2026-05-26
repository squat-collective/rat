module github.com/rat-data/rat-plugin-lineage

go 1.24

require (
	connectrpc.com/connect v1.18.1
	github.com/rat-data/rat/platform v0.0.0
	golang.org/x/net v0.32.0
)

replace github.com/rat-data/rat/platform => /platform
