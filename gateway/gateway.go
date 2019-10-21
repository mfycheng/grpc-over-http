package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"path"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	streamDesc = &grpc.StreamDesc{
		StreamName: "gateway_forwarder",

		// Just assume all streams are bidirectional.
		// todo: there _could_ be some gains by having proper insight into the streaming directions,
		//       but likely quite small since we don't have insight into request ordering, so it would
		//       just be being able to send early Close()'examples.
		ServerStreams: true,
		ClientStreams: true,
	}
)

// MuxOption configures the mux.
type MuxOption func(*Mux)

// WithUpgrader provides an alternative websocket.Upgrader to use.
func WithUpgrader(upgrader websocket.Upgrader) MuxOption {
	return func(m *Mux) {
		m.upgrader = upgrader
	}
}

// WithRouter provides an alternative mux.Router to use.
func WithRouter(router *mux.Router) MuxOption {
	return func(m *Mux) {
		m.router = router
	}
}

// Mux serves HTTP and Websocket endpoints that re-routes
type Mux struct {
	log *logrus.Entry

	cc       *grpc.ClientConn
	router   *mux.Router
	upgrader websocket.Upgrader
}

// New creates a new Mux that loads all registered services in the gRPC
// server and sets up corresponding routes.
//
// Unary requests are set up as basic HTTP/1.1 requests.
// Streaming requests are set up as Websocket connections.
func New(serv *grpc.Server, cc *grpc.ClientConn, opts ...MuxOption) *Mux {
	// todo: mux options
	m := &Mux{
		log:    logrus.WithField("type", "gateway/mux"),
		cc:     cc,
		router: mux.NewRouter(),
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 0,
			ReadBufferSize:   1024,
			WriteBufferSize:  1024,
		},
	}

	for _, o := range opts {
		o(m)
	}

	for service, info := range serv.GetServiceInfo() {
		for _, method := range info.Methods {
			fullMethod := fmt.Sprintf("%s/%s", service, method.Name)
			httpPath := path.Join("/api", fullMethod)

			if method.IsServerStream || method.IsClientStream {
				m.router.HandleFunc(httpPath, m.streamHandler(fullMethod))
			} else {
				m.router.HandleFunc(httpPath, m.unaryHandler(fullMethod))
			}
		}
	}

	return m
}

// ServeHTTP serves HTTP on the provided listener, forwarding requests
// to the gRPC server.
func (m *Mux) ServeHTTP(l net.Listener) error {
	return http.Serve(l, m.router)
}

// ServeHTTP listens on the specified address, and forwards requests
// to the gRPC server.
func (m *Mux) ListenAndServeHTTP(listenAddr string) error {
	return http.ListenAndServe(listenAddr, m.router)
}

func (m *Mux) unaryHandler(fullMethod string) http.HandlerFunc {
	log := m.log.WithFields(logrus.Fields{
		"method":    fullMethod,
		"streaming": "false",
	})

	// todo: proper header propagation
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			http.Error(w, "", http.StatusMethodNotAllowed)
			return
		}
		if req.Header.Get("Content-type") != "application/proto" {
			http.Error(w, "", http.StatusBadRequest)
			return
		}

		b, err := ioutil.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			// The two primary sources of errors are:
			//
			//     1. Client side connection closed / issues, or
			//     2. We had an issue allocating enough buffer room to handle the request.
			//
			// In both cases, we mostly expect the client to try again, so we return
			// with an 500. If the size they're sending is in general just too large,
			// we should most likely be attempting to reject that ahead of time.
			log.WithError(err).Trace("Failed to ready request body")
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		resp := new([]byte)
		if err = m.cc.Invoke(req.Context(), fullMethod, b, resp); err != nil {
			s, ok := status.FromError(err)
			if !ok {
				// In this case, the gateway setup has likely been mis-configured.
				http.Error(w, "gateway error", http.StatusBadGateway)
				return
			}

			http.Error(w, s.Message(), runtime.HTTPStatusFromCode(s.Code()))
			return
		}

		if n, err := io.Copy(w, bytes.NewBuffer(*resp)); err != nil {
			// Note: we _probably_ don't need to send back an error here, since the
			// connection is most likely dead
			log.WithError(err).Infof("Failed to send response (%d/%d transferred)", n, len(*resp))
		}
	}
}

func (m *Mux) streamHandler(fullMethod string) http.HandlerFunc {
	log := m.log.WithFields(logrus.Fields{
		"method":    fullMethod,
		"streaming": "true",
	})

	// todo: proper header propagation
	return func(w http.ResponseWriter, req *http.Request) {
		ws, err := m.upgrader.Upgrade(w, req, nil)
		if err != nil {
			log.WithError(err).Info("Failed to upgrade connection")
			return
		}
		defer ws.Close()

		streamCtx, cancelFunc := context.WithCancel(req.Context())
		defer cancelFunc()
		cs, err := m.cc.NewStream(streamCtx, streamDesc, fullMethod)
		if err != nil {
			log.WithError(err).Warn("Failed to initialize grpc stream")
			if err := ws.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, ""),
			); err != nil {
				log.WithError(err).Warn("Failed to write close message")
			}
			return
		}

		// note: we put the read loop in a separate goroutine rather than the write loop
		// because clients have no way of performing a CloseSend() equivalent.
		readErrCh := make(chan error, 1)
		go func() {
			defer close(readErrCh)

			for {
				_, data, err := ws.ReadMessage()
				if err != nil {
					// Reads from clients tend to be a connection issue, in which case
					// sending back an error doesn't usually make it back. However, if
					// there was a server error (i.e. failed to allocate enough memory
					// for the read), then it's a server error.
					//
					// Since sending back client type errors won't likely make it back,
					// we just treat it as an server error. Practically, this just means
					// we don't have to differentiate between errors here.
					readErrCh <- err
					return
				}

				if err := cs.SendMsg(data); err != nil {
					readErrCh <- err
					return
				}
			}
		}()

		respBytes := new([]byte)
		for {
			select {
			case err = <-readErrCh:
				break
			default:
			}

			if err = cs.RecvMsg(respBytes); err != nil {
				break
			}

			if err = ws.WriteMessage(websocket.BinaryMessage, *respBytes); err != nil {
				break
			}
		}

		// We _could_ differentiate between ws and cs errors here, but it's likely more overhead
		// to split the two vs trying to just blind write the error code, which at worst case writes
		// to a closed ws.
		if err := ws.WriteMessage(
			websocket.CloseMessage,
			grpcStatusToCloseMessage(err),
		); err != nil {
			log.WithError(err).Trace("Failed to write error status")
		}
	}
}

// grpcStatusToCloseMessage returns a websocket close
// message that 'wraps' a gRPC status.
func grpcStatusToCloseMessage(err error) []byte {
	if err == nil || err == io.EOF {
		return websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	}

	s, ok := status.FromError(err)
	if !ok {
		return websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "")
	}

	// The 4000-4999 range of status codes are reserved for private use, so we
	// simply take 4000 and add the gRPC status code onto it to allow clients to
	// have a better interpretation of what'examples going on.
	//
	// See: https://tools.ietf.org/html/rfc6455#section-7.4.1
	return websocket.FormatCloseMessage(4000+int(s.Code()), s.Message())
}
