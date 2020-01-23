package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/pkg/errors"
	"google.golang.org/grpc"

	"mfycheng.dev/grpc-over-http/examples/echo"
)

var (
	stream = flag.Bool("stream", false, "Use streaming API")
	status = flag.Int("status", 0, "Induce an error")
)

func run() error {
	cc, err := grpc.Dial("localhost:8080", grpc.WithInsecure())
	if err != nil {
		return errors.Wrap(err, "failed to dial")
	}

	client := echo.NewEchoClient(cc)
	if !*stream {
		resp, err := client.Echo(context.Background(), &echo.EchoRequest{
			Message:     "hello",
			Repetitions: 2,
			StatusCode:  int32(*status),
		})
		if err != nil {
			return errors.Wrap(err, "failed to send RPC call")
		}

		fmt.Println(resp)
		return nil
	}

	stream, err := client.EchoStream(context.Background(), &echo.EchoStreamRequest{
		Message:      "stream",
		Repetitions:  3,
		Responses:    10,
		StatusCode:   int32(*status),
		FailureIndex: 2,
		Interval:     ptypes.DurationProto(1 * time.Second),
	})
	if err != nil {
		return errors.Wrap(err, "failed to create stream")
	}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}

		fmt.Println(resp)
	}

}

func main() {
	flag.Parse()

	if err := run(); err != nil {
		log.Fatal(err)
	}
}
