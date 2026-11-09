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
	operations  sync.Mutex
	mutations   int
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
	s.operations.Lock()
	s.stopping.Store(true)
	s.operations.Unlock()
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
	session    string
	logs       *engine.MCPLogReader
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
	if json.Unmarshal(req.Params, &hello) != nil || hello.APIVersion < 1 || !knownClientKind(hello.Client.Kind) || hello.Client.Name == "" {
		c.respond(req.ID, nil, api.NewError(api.Incompatible, "invalid daemon.hello"))
		return
	}
	c.compatible = hello.APIVersion == api.Version
	c.apiVersion = hello.APIVersion
	c.client = hello.Client
	session, err := c.server.engine.ConnectSession(api.Actor{Kind: c.client.Kind, Name: c.client.Name}, c.client.Session)
	if err != nil {
		c.respond(req.ID, nil, api.NewError(api.Incompatible, err.Error()))
		return
	}
	c.session = session
	c.logs = engine.NewMCPLogReader()
	defer func() {
		// OS work already in flight must not block server/connection shutdown.
		go func() {
			_ = c.server.engine.DisconnectSession(api.Actor{Kind: c.client.Kind, Name: c.client.Name}, c.session)
		}()
	}()
	result := c.server.Hello()
	result.Session = session
	c.respond(req.ID, result, nil)
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
				if c.client.Kind == "mcp" && !c.server.engine.AgentEnabled() {
					continue
				}
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
		// OS operations may outlive the connection; shutdown must not wait for their handlers.
		go func() { defer func() { <-slots }(); c.execute(req) }()
	}
}

func (c *connection) enqueue(event api.Event) {
	if c.client.Kind == "mcp" && !c.server.engine.AgentEnabled() {
		return
	}
	if c.client.Kind == "mcp" && event.Prompt != nil {
		prompt := *event.Prompt
		prompt.Prompt.Text = ""
		event.Prompt = &prompt
	}
	if c.client.Kind == "mcp" && event.Type == api.EventApprovalAdded {
		return
	}
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
	if s.stopping.Load() {
		c.respond(req.ID, nil, api.NewError(api.Stopped, "daemon is shutting down"))
		return
	}
	if c.client.Kind == "mcp" && req.Method != api.DaemonHello && req.Method != api.DaemonStatusMethod && req.Method != "policy.get" {
		if err := s.engine.AgentAccess(api.Actor{Kind: c.client.Kind, Name: c.client.Name}, c.session); err != nil {
			c.respond(req.ID, nil, err)
			return
		}
	}
	if c.client.Kind == "mcp" && !mcpMethodAllowed(req.Method) {
		c.respond(req.ID, nil, s.engine.PolicyRefusalForSession(api.Actor{Kind: c.client.Kind, Name: c.client.Name}, c.session))
		return
	}
	if !c.compatible && req.Method != api.DaemonStatusMethod && req.Method != api.DaemonShutdown {
		c.respond(req.ID, nil, api.NewError(api.Incompatible, fmt.Sprintf("client API %d is incompatible with daemon API %d; run devt-tunnel daemon restart (stops all tunnels)", c.apiVersion, api.Version)))
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
		if s.stopping.Load() {
			s.operations.Unlock()
			c.respond(req.ID, nil, api.NewError(api.Stopped, "daemon is shutting down"))
			return
		}
		if p.ExpectedPID != 0 && p.ExpectedPID != s.Hello().PID {
			s.operations.Unlock()
			c.respond(req.ID, nil, api.NewError(api.Conflict, "daemon identity changed"))
			return
		}
		if p.OnlyIfIdle && (s.mutations > 0 || s.engine.Active()) {
			s.operations.Unlock()
			c.respond(req.ID, nil, api.NewError(api.Conflict, "daemon has active tunnels, prompts or mutations"))
			return
		}
		s.stopping.Store(true)
		s.operations.Unlock()
		s.engine.Shutdown()
		c.respond(req.ID, api.Empty{}, nil)
		if s.onShutdown != nil {
			s.onShutdown()
		}
		return
	}
	s.operations.Lock()
	if s.stopping.Load() {
		s.operations.Unlock()
		c.respond(req.ID, nil, api.NewError(api.Stopped, "daemon is shutting down"))
		return
	}
	if !s.readOnly[req.Method] {
		s.mutations++
		defer func() {
			s.operations.Lock()
			s.mutations--
			s.operations.Unlock()
		}()
	}
	s.operations.Unlock()
	h, ok := s.handlers[req.Method]
	if !ok {
		c.respond(req.ID, nil, &api.Error{Code: -32601, Message: "method not found", Data: api.ErrorData{Kind: api.Unknown}})
		return
	}
	ctx := context.WithValue(c.ctx, connectionKey{}, c)
	if err := ctx.Err(); err != nil {
		c.respond(req.ID, nil, err)
		return
	}
	result, err := h(ctx, req.Params)
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

type connectionKey struct{}

// Only "mcp" is restricted, so an unrecognized kind such as "MCP" must not
// slip past the allow-list as an unrestricted client.
func knownClientKind(kind string) bool { return kind == "gui" || kind == "cli" || kind == "mcp" }

// This deliberately constant allow-list is the MCP security boundary. Never
// derive it from handler registration or readOnly (new methods default-deny).
func mcpMethodAllowed(method string) bool {
	switch method {
	case api.DaemonHello, api.DaemonStatusMethod, api.EventsSubscribe,
		"tunnel.openRequest", "tunnel.waitRequest", "tunnel.checkRequest", "policy.get",
		"tunnel.list", "tunnel.get", "tunnel.waitReady", "tunnel.findings", "tunnel.evaluateDraft",
		"ssh.hosts", "ssh.resolve", "tool.list", "prompt.list",
		"lease.open", "lease.renew", "lease.close", "lease.list", "tunnel.runEphemeral",
		"approval.list", "approval.get", "logs.read", "logs.since", "tunnel.promote", "tunnel.save":
		return true
	}
	return false
}

func connectionActor(ctx context.Context) (api.Actor, string) {
	c := ctx.Value(connectionKey{}).(*connection)
	return api.Actor{Kind: c.client.Kind, Name: c.client.Name}, c.session
}
