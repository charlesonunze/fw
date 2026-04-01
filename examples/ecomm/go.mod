module github.com/charlesonunze/fw/examples/ecomm

go 1.25.2

require (
	github.com/charlesonunze/fw v0.0.0
	github.com/go-chi/chi/v5 v5.2.5
	github.com/go-chi/render v1.0.3
	google.golang.org/grpc v1.79.1
	google.golang.org/protobuf v1.36.10
)

require (
	github.com/ajg/form v1.5.1 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
)

replace github.com/charlesonunze/fw => ../../
