// Package client connects GUI and headless clients to the per-user daemon.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

type Client struct {
	conn     net.Conn
	hello    api.HelloResult
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	next     uint64
	pending  map[string]chan api.Response
	readErr  error
	callback func(api.Event)
	writeMu  sync.Mutex
	events   chan api.Event
}

func Dial(ctx context.Context, socket string, info api.ClientInfo) (*Client, error) {
	c, err := DialManagement(ctx, socket, info)
	if err != nil {
		return nil, err
	}
	if c.hello.APIVersion != api.Version {
		c.Close()
		return nil, api.VersionError(c.hello.APIVersion)
	}
	return c, nil
}

// DialManagement permits status and shutdown across API versions for recovery.
func DialManagement(ctx context.Context, socket string, info api.ClientInfo) (*Client, error) {
	return dialManagement(ctx, socket, info, platform.CheckPeer)
}

func dialManagement(ctx context.Context, socket string, info api.ClientInfo, checkPeer func(net.Conn) error) (*Client, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, err
	}
	if err = checkPeer(conn); err != nil {
		conn.Close()
		return nil, err
	}
	if err = platform.CheckPrivateDir(filepath.Dir(socket)); err != nil {
		conn.Close()
		return nil, err
	}
	c := &Client{conn: conn, done: make(chan struct{}), pending: map[string]chan api.Response{}, events: make(chan api.Event, 64)}
	go c.read()
	go c.dispatch()
	if err = c.Call(ctx, api.DaemonHello, api.HelloParams{APIVersion: api.Version, Client: info}, &c.hello); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Session is the reconnect token to pass in ClientInfo.Session on the next dial.
func (c *Client) Session() string        { return c.hello.Session }
func (c *Client) Hello() api.HelloResult { return c.hello }
func (c *Client) Done() <-chan struct{}  { return c.done }
func (c *Client) Close() error           { c.once.Do(func() { close(c.done); c.conn.Close() }); return nil }

// Events installs a callback; call events.subscribe after installing it, then resync on reconnect.
func (c *Client) Events(callback func(api.Event)) { c.mu.Lock(); c.callback = callback; c.mu.Unlock() }

func (c *Client) dispatch() {
	for {
		select {
		case <-c.done:
			return
		case event := <-c.events:
			c.mu.Lock()
			callback := c.callback
			c.mu.Unlock()
			if callback != nil {
				callback(event)
			}
		}
	}
}

func (c *Client) read() {
	defer c.Close()
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 4096), api.MaxMessageSize)
	for scanner.Scan() {
		var envelope struct {
			api.Response
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &envelope) != nil || envelope.JSONRPC != "2.0" {
			return
		}
		if envelope.Method != "" {
			var event api.Event
			if json.Unmarshal(envelope.Params, &event) != nil {
				return
			}
			select {
			case c.events <- event:
			case <-c.done:
				return
			default:
				return
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[string(envelope.ID)]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- envelope.Response:
			default:
			}
		}
	}
	if err := scanner.Err(); err != nil {
		c.mu.Lock()
		c.readErr = err
		c.mu.Unlock()
	}
}

func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.next++
	id := strconv.FormatUint(c.next, 10)
	ch := make(chan api.Response, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	request := api.Request{JSONRPC: "2.0", ID: json.RawMessage(id), Method: method, Params: raw}
	c.writeMu.Lock()
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	err = c.conn.SetWriteDeadline(deadline)
	if err == nil {
		err = json.NewEncoder(c.conn).Encode(request)
	}
	c.writeMu.Unlock()
	if err != nil {
		c.Close()
		return contextIOError(ctx, err)
	}
	select {
	case response := <-ch:
		return decode(response, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		// The reader can receive the shutdown reply and EOF in the same scheduling turn.
		select {
		case response := <-ch:
			return decode(response, result)
		default:
			c.mu.Lock()
			err := c.readErr
			c.mu.Unlock()
			if err != nil {
				return contextIOError(ctx, err)
			}
			return io.EOF
		}
	}
}

// A socket deadline can expire before the context timer goroutine publishes Err.
// Report the context outcome consistently, but keep independent I/O timeouts.
func contextIOError(ctx context.Context, err error) error {
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		return err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return err
}

func decode(response api.Response, result any) error {
	if response.Error != nil {
		return response.Error
	}
	if len(response.Result) == 0 {
		return errors.New("RPC response has no result")
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(response.Result, result)
}
