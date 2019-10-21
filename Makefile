.PHONY: generate-examples
generate-examples:
	@protoc --go_out=plugins=grpc:. examples/echo/*.proto

.PHONY: deps
deps:
	@go get ./...

.PHONY: test
test:
	@go test -v ./...

