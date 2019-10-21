package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/gorilla/websocket"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/mfycheng/grpc-over-http/examples/echo"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUnknown(t *testing.T) {
	addr, cleanup := setup(t)
	defer cleanup()

	b, err := proto.Marshal(&echo.EchoRequest{
		Message:     "hello",
		Repetitions: 3,
	})
	require.NoError(t, err)

	httpResp, err := http.Post(
		fmt.Sprintf("http://%s/api/echo.v1.Echo/Nope", addr),
		"application/proto",
		bytes.NewBuffer(b),
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, httpResp.StatusCode)
}

func TestUnary_Happy(t *testing.T) {
	addr, cleanup := setup(t)
	defer cleanup()

	b, err := proto.Marshal(&echo.EchoRequest{
		Message:     "hello",
		Repetitions: 3,
	})
	require.NoError(t, err)

	httpResp, err := http.Post(
		fmt.Sprintf("http://%s/api/echo.v1.Echo/Echo", addr),
		"application/proto",
		bytes.NewBuffer(b),
	)
	require.NoError(t, err)

	respBytes, err := ioutil.ReadAll(httpResp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, httpResp.StatusCode)

	resp := &echo.EchoResponse{}
	require.NoError(t, proto.Unmarshal(respBytes, resp))
	assert.Equal(t, "hellohellohello", resp.Message)
}

func TestUnary_ErrorCodes(t *testing.T) {
	addr, cleanup := setup(t)
	defer cleanup()

	b, err := proto.Marshal(&echo.EchoRequest{
		Message:     "hello",
		Repetitions: 3,
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		method      string
		contentType string
		status      int
	}{
		{
			"GET",
			"application/proto",
			http.StatusMethodNotAllowed,
		},
		{
			"POST",
			"application/json",
			http.StatusBadRequest,
		},
	} {
		req, err := http.NewRequest(tc.method, fmt.Sprintf("http://%s/api/echo.v1.Echo/Echo", addr), bytes.NewReader(b))
		require.NoError(t, err)
		req.Header.Set("Content-type", tc.contentType)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		assert.Equal(t, tc.status, resp.StatusCode)
	}

	// gRPC to status errors
	for i := codes.Canceled; i <= codes.Unauthenticated; i++ {
		b, err := proto.Marshal(&echo.EchoRequest{
			Message:     "hello",
			Repetitions: 3,
			StatusCode:  int32(i),
		})
		require.NoError(t, err)

		httpResp, err := http.Post(
			fmt.Sprintf("http://%s/api/echo.v1.Echo/Echo", addr),
			"application/proto",
			bytes.NewBuffer(b),
		)
		require.NoError(t, err)

		assert.Equal(t, runtime.HTTPStatusFromCode(i), httpResp.StatusCode)
	}
}

func TestStream_Happy(t *testing.T) {
	addr, cleanup := setup(t)
	defer cleanup()

	b, err := proto.Marshal(&echo.EchoStreamRequest{
		Message:     "hello",
		Repetitions: 2,
		Responses:   3,
		Interval:    ptypes.DurationProto(50 * time.Millisecond),
	})
	require.NoError(t, err)

	conn, _, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://%s/api/echo.v1.Echo/EchoStream", addr),
		nil,
	)
	require.NoError(t, err)

	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, b))

	for i := 0; i < 3; i++ {
		mType, data, err := conn.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, mType, websocket.BinaryMessage)

		resp := &echo.EchoStreamResponse{}
		require.NoError(t, proto.Unmarshal(data, resp))

		require.Equal(t, "hellohello", resp.Message)
		require.EqualValues(t, i, resp.Index)
	}

	assert.NoError(t, conn.Close())
}

func TestStream_ErrorCodes(t *testing.T) {
	addr, cleanup := setup(t)
	defer cleanup()

	// gRPC to status errors
	for i := codes.Canceled; i <= codes.Unauthenticated; i++ {
		b, err := proto.Marshal(&echo.EchoStreamRequest{
			Message:      "hello",
			Repetitions:  2,
			Responses:    10,
			Interval:     ptypes.DurationProto(50 * time.Millisecond),
			StatusCode:   int32(i),
			FailureIndex: 1,
		})
		require.NoError(t, err)
		conn, _, err := websocket.DefaultDialer.Dial(
			fmt.Sprintf("ws://%s/api/echo.v1.Echo/EchoStream", addr),
			nil,
		)
		require.NoError(t, err)

		require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, b))

		_, _, err = conn.ReadMessage()
		require.NoError(t, err, "code: %d", i)

		_, _, err = conn.ReadMessage()
		assert.True(t, websocket.IsCloseError(err, 4000+int(i)))
	}
}

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

func setup(t *testing.T) (addr string, cleanup func()) {
	s := grpc.NewServer()
	echo.RegisterEchoServer(s, &serv{})

	gl, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	port := gl.Addr().(*net.TCPAddr).Port
	cc, err := grpc.Dial(
		fmt.Sprintf("localhost:%d", port),
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(&BinaryCodec{})),
	)
	require.NoError(t, err)

	m := New(s, cc)

	hl, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	go func() {
		if err := s.Serve(gl); err != nil {
			require.Equal(t, err, grpc.ErrServerStopped)
		}
	}()
	go func() {
		if err := m.ServeHTTP(hl); err != nil {
			// Since we don't expose or use an http.Server directly, we
			// don't have a mechanism to cleanly shutdown the HTTP server,
			// so we just log it in case there are troubles.
			log.WithError(err).Trace("HTTP server closed with failure")
		}
	}()

	return hl.Addr().String(), func() {
		hl.Close()

		s.Stop()
		gl.Close()
	}
}
