// Package control serves the same-user, newline-delimited JSON-RPC API.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/engine"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

type handler func(context.Context, json.RawMessage) (any, error)
type Server struct {
	engine      *engine.Engine
	version     string
	handlers    map[string]handler
	readOnly    map[string]bool
	onShutdown  func()
	mu          sync.Mutex
	connections map[*connection]struct{}
	listener    net.Listener
	closed      bool
	wg          sync.WaitGroup
	operations  sync.RWMutex
	stopping    atomic.Bool
}

func New(e *engine.Engine, version string, onShutdown func()) *Server {
	s := &Server{engine: e, version: version, onShutdown: onShutdown, handlers: map[string]handler{}, readOnly: map[string]bool{}, connections: map[*connection]struct{}{}}
	s.register()
	return s
}

func handle[P, R any](s *Server, method string, fn func(context.Context, P) (R, error)) {
	if _, exists := s.handlers[method]; exists {
		panic("duplicate RPC method: " + method)
	}
	s.handlers[method] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p P
		if len(raw) != 0 && string(raw) != "null" {
			if raw[0] != '{' || json.Unmarshal(raw, &p) != nil {
				return nil, &api.Error{Code: -32602, Message: "invalid parameters", Data: api.ErrorData{Kind: api.Unknown}}
			}
		}
		return fn(ctx, p)
	}
}

func (s *Server) Hello() api.HelloResult {
	return api.HelloResult{APIVersion: api.Version, DaemonVersion: s.version, PID: os.Getpid()}
}
func (s *Server) Connections() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.connections) }

func (s *Server) Serve(listener net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return net.ErrClosed
	}
	s.listener = listener
	s.mu.Unlock()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if err := platform.CheckPeer(conn); err != nil {
			conn.Close()
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		c := &connection{server: s, conn: conn, ctx: ctx, cancel: cancel, events: make(chan api.Event, 32)}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			c.close()
			return nil
		}
		s.connections[c] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go func() { defer s.wg.Done(); c.serve(); s.mu.Lock(); delete(s.connections, c); s.mu.Unlock() }()
	}
}

func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	if s.listener != nil {
		s.listener.Close()
	}
	for c := range s.connections {
		c.close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

type connection struct {
	server     *Server
	conn       net.Conn
	ctx        context.Context
	cancel     context.CancelFunc
	writeMu    sync.Mutex
	events     chan api.Event
	subscribed atomic.Bool
	compatible bool
	apiVersion int
	client     api.ClientInfo
}

func (c *connection) close() { c.cancel(); c.conn.Close() }
func (c *connection) write(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	return json.NewEncoder(c.conn).Encode(v)
}

func (c *connection) respond(id json.RawMessage, result any, err error) {
	if len(id) == 0 {
		return
	}
	response := api.Response{JSONRPC: "2.0", ID: id}
	if err != nil {
		response.Error = wireError(err)
	} else {
		response.Result, err = json.Marshal(result)
		if err != nil {
			response.Error = wireError(err)
		}
	}
	if c.write(response) != nil {
		c.close()
	}
}

func (c *connection) serve() {
	defer c.close()
	// A half-open connection must not indefinitely prevent idle exit.
	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 4096), api.MaxMessageSize)
	if !scanner.Scan() {
		return
	}
	req, err := parseRequest(scanner.Bytes())
	if err != nil {
		c.respond(json.RawMessage("null"), nil, err)
		return
	}
	if req.Method != api.DaemonHello || len(req.ID) == 0 {
		c.respond(req.ID, nil, api.NewError(api.Incompatible, "daemon.hello must be the first request"))
		return
	}
	var hello api.HelloParams
	if json.Unmarshal(req.Params, &hello) != nil || hello.APIVersion < 1 || hello.Client.Kind == "" || hello.Client.Name == "" {
		c.respond(req.ID, nil, api.NewError(api.Incompatible, "invalid daemon.hello"))
		return
	}
	c.compatible = hello.APIVersion == api.Version
	c.apiVersion = hello.APIVersion
	c.client = hello.Client
	c.respond(req.ID, c.server.Hello(), nil)
	_ = c.conn.SetReadDeadline(time.Time{})
	unsubscribe := c.server.engine.Subscribe(c.enqueue)
	defer unsubscribe()
	var workers sync.WaitGroup
	defer workers.Wait()
	defer c.cancel()
	workers.Go(func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case event := <-c.events:
				params, _ := json.Marshal(event)
				if c.write(api.Request{JSONRPC: "2.0", Method: event.Type, Params: params}) != nil {
					c.close()
					return
				}
			}
		}
	})
	slots := make(chan struct{}, 128)
	for scanner.Scan() {
		req, err := parseRequest(scanner.Bytes())
		if err != nil {
			c.respond(json.RawMessage("null"), nil, err)
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			return
		}
		run := func() { defer func() { <-slots }(); c.execute(req) }
		if c.server.readOnly[req.Method] {
			// OS keychain reads cannot be cancelled and must not keep a disconnected client or daemon alive.
			go run()
		} else {
			workers.Go(run)
		}
	}
}

func (c *connection) enqueue(event api.Event) {
	if !c.subscribed.Load() {
		return
	}
	select {
	case c.events <- event:
	case <-c.ctx.Done():
	default:
		c.close()
	}
}

func parseRequest(line []byte) (api.Request, error) {
	var req api.Request
	if json.Unmarshal(line, &req) != nil {
		return req, &api.Error{Code: -32700, Message: "invalid JSON", Data: api.ErrorData{Kind: api.Unknown}}
	}
	validID := len(req.ID) == 0 || req.ID[0] == '"' || (req.ID[0] >= '0' && req.ID[0] <= '9') || req.ID[0] == '-'
	if req.JSONRPC != "2.0" || req.Method == "" || !validID {
		return req, &api.Error{Code: -32600, Message: "invalid request", Data: api.ErrorData{Kind: api.Unknown}}
	}
	return req, nil
}

func (c *connection) execute(req api.Request) {
	s := c.server
	if !c.compatible && req.Method != api.DaemonStatusMethod && req.Method != api.DaemonShutdown {
		c.respond(req.ID, nil, api.NewError(api.Incompatible, fmt.Sprintf("client API %d is incompatible with daemon API %d; run dtt daemon restart (stops all tunnels)", c.apiVersion, api.Version)))
		return
	}
	if req.Method == api.DaemonHello {
		c.respond(req.ID, nil, api.NewError(api.Incompatible, "daemon.hello is only allowed once"))
		return
	}
	if req.Method == api.EventsSubscribe {
		c.subscribed.Store(true)
		c.respond(req.ID, api.Empty{}, nil)
		return
	}
	if req.Method == api.DaemonShutdown {
		var p api.ShutdownParams
		if len(req.Params) > 0 && json.Unmarshal(req.Params, &p) != nil {
			c.respond(req.ID, nil, api.NewError(api.Unknown, "invalid parameters"))
			return
		}
		s.operations.Lock()
		if p.OnlyIfIdle && s.engine.Active() {
			s.operations.Unlock()
			c.respond(req.ID, nil, api.NewError(api.Conflict, "daemon has active tunnels or prompts"))
			return
		}
		s.stopping.Store(true)
		s.engine.Shutdown()
		s.operations.Unlock()
		c.respond(req.ID, api.Empty{}, nil)
		if s.onShutdown != nil {
			s.onShutdown()
		}
		return
	}
	if !s.readOnly[req.Method] {
		s.operations.RLock()
		defer s.operations.RUnlock()
	}
	if s.stopping.Load() {
		c.respond(req.ID, nil, api.NewError(api.Stopped, "daemon is shutting down"))
		return
	}
	if c.client.Kind == "mcp" && req.Method == "prompt.answer" {
		c.respond(req.ID, nil, api.NewError(api.Unknown, "MCP clients cannot answer prompts"))
		return
	}
	h, ok := s.handlers[req.Method]
	if !ok {
		c.respond(req.ID, nil, &api.Error{Code: -32601, Message: "method not found", Data: api.ErrorData{Kind: api.Unknown}})
		return
	}
	result, err := h(c.ctx, req.Params)
	c.respond(req.ID, result, err)
}

func wireError(err error) *api.Error {
	var rpc *api.Error
	if errors.As(err, &rpc) {
		return rpc
	}
	kind := api.Internal
	switch {
	case errors.Is(err, config.ErrConflict):
		kind = api.Conflict
	case errors.Is(err, supervisor.ErrNeedsSecret), errors.Is(err, secret.ErrNotFound):
		kind = api.NeedsSecret
	case errors.Is(err, secret.ErrUnavailable):
		kind = api.KeyringUnavailable
	case errors.Is(err, supervisor.ErrStopped), errors.Is(err, context.Canceled), errors.Is(err, io.EOF):
		kind = api.Stopped
	case errors.Is(err, context.DeadlineExceeded):
		kind = api.Timeout
	case strings.HasPrefix(err.Error(), "unknown "):
		kind = api.Unknown
	}
	return api.NewError(kind, err.Error())
}
