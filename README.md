# grpc-over-http [![CircleCI](https://circleci.com/gh/mfycheng/grpc-over-http.svg?style=svg)](https://circleci.com/gh/mfycheng/grpc-over-http) [![Documentation](https://godoc.org/mfycheng.dev/grpc-over-http?status.svg)](http://godoc.org/mfycheng.dev/grpc-over-http)

gRPC over http is a very simple proxy that simply forwards HTTP/1.1 and Websocket
requests to a gRPC server. Unlike some of the bigger frameworks out there (notably
[grpc-gateway](https://github.com/grpc-ecosystem/grpc-gateway)), grpc-over-http doesn't
offer any types of transforms or path remapping, it simply forwards raw protobufs to
the gRPC server.

It is expected to be run in the same process as gRPC, to avoid having to change run
any additional infrastructure, or have modifications to any of the proto definitions.

## Motivation

The primary motivation of grpc-over-http is for environments or setups where HTTP/2
or gRPC libraries are not fully supported. Some examples include nlegacy Load Balancers,
or some multi-platform client libraries.

## Protocol

The path for both unary and streaming requests are: `/api/<service>/<Method>`

### Unary Requests

Unary requests

* **Method**: `POST`
* **Content-type**: `application/protobuf`
* **Body**: `<raw-proto-bytes>`

### Streaming Requests (client, server, or bidirectional)

Streaming requests use websockets, where the payloads in
both directions are binary messages, containing the raw
proto payload.
