package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"

	"mfycheng.dev/grpc-over-http/examples/echo"
)

var (
	stream = flag.Bool("stream", false, "Use streaming API")
	status = flag.Int("status", 0, "Induce an error")
)

func run() error {
	if !*stream {
		b, err := proto.Marshal(&echo.EchoRequest{
			Message:     "hello",
			Repetitions: 2,
			StatusCode:  int32(*status),
		})
		if err != nil {
			return errors.Wrap(err, "failed to marshal request")
		}

		httpResp, err := http.Post("http://localhost:8085/api/echo.v1.Echo/Echo", "application/proto", bytes.NewBuffer(b))
		if err != nil {
			return errors.Wrapf(err, "failed to post")
		}

		respBytes, err := ioutil.ReadAll(httpResp.Body)
		if err != nil {
			return errors.Wrapf(err, "failed to read body")
		}
		if httpResp.StatusCode != http.StatusOK {
			return errors.Errorf("unexpected code: %d: %s", httpResp.StatusCode, string(respBytes))
		}

		resp := &echo.EchoResponse{}
		if err := proto.Unmarshal(respBytes, resp); err != nil {
			return errors.Wrapf(err, "failed to parse body")
		}

		fmt.Println(resp)
		return nil
	}

	ws, _, err := websocket.DefaultDialer.Dial("ws://localhost:8085/api/echo.v1.Echo/EchoStream", nil)
	if err != nil {
		return errors.Wrap(err, "failed to create websocket")
	}

	b, err := proto.Marshal(&echo.EchoStreamRequest{
		Message:     "hello",
		Repetitions: 10,
		Responses:   10,
		StatusCode:  int32(*status),
		Interval:    ptypes.DurationProto(1 * time.Second),
	})
	if err != nil {
		return errors.Wrapf(err, "failed to serialize request")
	}

	if err := ws.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return errors.Wrap(err, "failed to send request")
	}

	for {
		_, b, err := ws.ReadMessage()
		if err != nil {
			return errors.Wrap(err, "failed to read message")
		}

		resp := &echo.EchoStreamResponse{}
		if err := proto.Unmarshal(b, resp); err != nil {
			return errors.Wrapf(err, "failed to parse message")
		}

		fmt.Println(resp)
	}

	return nil
}

func main() {
	flag.Parse()

	if err := run(); err != nil {
		log.Fatal(err)
	}
}
