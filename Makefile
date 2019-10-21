.PHONY: generate-examples
generate-examples:
	@protoc --go_out=plugins=grpc:. examples/echo/*.proto

.PHONY: test
test:
	@go test -v ./...

