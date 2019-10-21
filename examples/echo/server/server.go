package main

import (
	"context"
	"io"
	"net"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/mfycheng/grpc-over-http/examples/echo"
	"github.com/mfycheng/grpc-over-http/gateway"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type serv struct{}

func (s serv) Echo(_ context.Context, req *echo.EchoRequest) (*echo.EchoResponse, error) {
	if req.StatusCode != 0 {
		return nil, status.Error(codes.Code(req.StatusCode), "induce")
	}

	return &echo.EchoResponse{
		Message: strings.Repeat(req.Message, int(req.Repetitions)),
	}, nil
}

func (s serv) EchoStream(req *echo.EchoStreamRequest, stream echo.Echo_EchoStreamServer) error {
	for i := 0; i < int(req.Responses); i++ {
		if req.StatusCode != 0 && int(req.FailureIndex) == i {
			return status.Error(codes.Code(req.StatusCode), "induced")
		}

		err := stream.Send(&echo.EchoStreamResponse{
			Message: strings.Repeat(req.Message, int(req.Repetitions)),
			Index:   uint64(i),
		})
		if err == io.EOF {
			log.Warnf("Stream terminated early (%d/%d)", i, req.Responses)
			return nil
		}

		interval, err := ptypes.Duration(req.Interval)
		if err != nil {
			log.WithError(err).Info("Failed to parse duration")
			return status.Error(codes.InvalidArgument, "bad duration")
		}
		time.Sleep(interval)
	}

	return nil
}

func run() error {
	s := grpc.NewServer()
	echo.RegisterEchoServer(s, &serv{})

	l, err := net.Listen("tcp", ":8080")
	if err != nil {
		return errors.Wrap(err, "failed to listen")
	}

	// todo: can we just move this inside?
	cc, err := grpc.Dial(
		"localhost:8080",
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(&gateway.BinaryCodec{})),
	)

	m := gateway.New(s, cc)

	go func() {
		log.Fatal(m.ListenAndServeHTTP(":8085"))
	}()

	return s.Serve(l)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
