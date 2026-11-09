// Package askpass connects the devt-tunnel-askpass helper, which ssh runs, to the app.
// With SSH_ASKPASS_REQUIRE=force every ssh prompt arrives here, so a stored passphrase
// answers only passphrase prompts; anything else goes to the user.
package askpass

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

const (
	EnvEndpoint = "DEVT_TUNNEL_ASKPASS_ENDPOINT"
	EnvToken    = "DEVT_TUNNEL_ASKPASS_TOKEN"
)

type Kind string

const (
	KindPassphrase Kind = "passphrase"
	KindPassword   Kind = "password"
	KindHostKey    Kind = "host_key"
	KindOther      Kind = "other"
)

// Classify identifies an ssh prompt from its text.
func Classify(prompt string) Kind {
	p := strings.ToLower(strings.TrimSpace(prompt))
	switch {
	case strings.HasPrefix(p, "enter passphrase for key '") && strings.HasSuffix(p, "':") && len(p) > len("enter passphrase for key '':") && !strings.ContainsAny(p, "\r\n"):
		return KindPassphrase
	case strings.Contains(p, "continue connecting"), strings.Contains(p, "fingerprint"):
		return KindHostKey
	case strings.Contains(p, "password"):
		return KindPassword
	}
	return KindOther
}

// Prompt is a question for the user.
type Prompt struct {
	TunnelID string `json:"tunnelId"`
	Text     string `json:"text"`
	Kind     Kind   `json:"kind"`
}

// Asker shows a prompt to the user and returns the answer.
type Asker func(ctx context.Context, p Prompt) (string, error)

type request struct {
	Token  string `json:"token"`
	Prompt string `json:"prompt"`
}

type response struct {
	Answer string `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Ask sends a prompt to the broker; the helper calls it.
func Ask(endpoint, token, prompt string) (string, error) {
	conn, err := net.DialTimeout("unix", endpoint, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request{Token: token, Prompt: prompt}); err != nil {
		return "", err
	}
	var res response
	if err := json.NewDecoder(conn).Decode(&res); err != nil {
		return "", err
	}
	if res.Error != "" {
		return "", errors.New(res.Error)
	}
	return res.Answer, nil
}

type registration struct {
	tunnelID string
	secret   string
	// A second passphrase prompt means the stored value was wrong; do not resend it.
	usedStored bool
}

// Broker answers helper requests over a private local socket.
type Broker struct {
	helper   string
	secrets  secret.Store
	ask      Asker
	timeout  time.Duration
	dir      string
	listener net.Listener

	mu   sync.Mutex
	regs map[string]*registration
	ctx  context.Context
	stop context.CancelFunc
}

// NewBroker starts listening. helperPath is the devt-tunnel-askpass binary.
func NewBroker(helperPath string, secrets secret.Store, ask Asker) (*Broker, error) {
	// A short temp path, because Unix socket paths are limited to about 100 bytes.
	dir, err := os.MkdirTemp("", "devt-tunnel")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	l, err := net.Listen("unix", filepath.Join(dir, "a.sock"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	ctx, stop := context.WithCancel(context.Background())
	b := &Broker{
		helper: helperPath, secrets: secrets, ask: ask, timeout: 2 * time.Minute,
		dir: dir, listener: l, regs: map[string]*registration{}, ctx: ctx, stop: stop,
	}
	go b.serve()
	return b, nil
}

// Close stops listening and removes the socket.
func (b *Broker) Close() error {
	b.stop()
	err := b.listener.Close()
	os.RemoveAll(b.dir)
	return err
}

// Register implements supervisor.Askpass.
func (b *Broker) Register(tunnelID, passphraseSecret string) ([]string, func()) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	token := hex.EncodeToString(raw[:])
	b.mu.Lock()
	b.regs[token] = &registration{tunnelID: tunnelID, secret: passphraseSecret}
	b.mu.Unlock()

	env := []string{
		"SSH_ASKPASS=" + b.helper,
		"SSH_ASKPASS_REQUIRE=force",
		EnvEndpoint + "=" + b.listener.Addr().String(),
		EnvToken + "=" + token,
	}
	// OpenSSH older than 8.4 ignores SSH_ASKPASS_REQUIRE and needs DISPLAY set to use askpass.
	if os.Getenv("DISPLAY") == "" {
		env = append(env, "DISPLAY=:0")
	}
	return env, func() {
		b.mu.Lock()
		delete(b.regs, token)
		b.mu.Unlock()
	}
}

func (b *Broker) serve() {
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}
		go b.handle(conn)
	}
}

func (b *Broker) handle(conn net.Conn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var req request
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
		return
	}
	conn.SetDeadline(time.Now().Add(b.timeout + 5*time.Second))
	answer, err := b.answer(req)
	res := response{Answer: answer}
	if err != nil {
		res = response{Error: err.Error()}
	}
	json.NewEncoder(conn).Encode(res)
}

func (b *Broker) answer(req request) (string, error) {
	b.mu.Lock()
	reg, ok := b.regs[req.Token]
	var useStored bool
	kind := Classify(req.Prompt)
	if ok && kind == KindPassphrase && reg.secret != "" && !reg.usedStored {
		reg.usedStored = true
		useStored = true
	}
	var tunnelID, secretName string
	if ok {
		tunnelID, secretName = reg.tunnelID, reg.secret
	}
	b.mu.Unlock()
	if !ok {
		return "", errors.New("unknown token")
	}
	if useStored {
		if value, err := b.secrets.Get(secretName); err == nil {
			return value, nil
		}
	}
	ctx, cancel := context.WithTimeout(b.ctx, b.timeout)
	defer cancel()
	return b.ask(ctx, Prompt{TunnelID: tunnelID, Text: req.Prompt, Kind: kind})
}
