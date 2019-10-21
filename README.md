# grpc-over-http

gRPC over http is a very simple proxy that simply forwards HTTP/1.1 and Websocket
requests to a gRPC server. Unlike some of the bigger frameworks out there (notably
[grpc-gateway](https://github.com/grpc-ecosystem/grpc-gateway), grpc-over-http doesn't
offer any types of transforms or path remapping, it simply forwards raw protobufs to
the gRPC server.

It is expected to be run in the same process as gRPC, to avoid having to change run
any additional infrastructure, or have modifications to any of the proto definitions.

## Motivation

The primary motivation of grpc-over-http is for environments or setups where HTTP/2
or gRPC libraries are not fully supported. Some examples include nlegacy Load Balancers,
or some multi-platform client libraries.

