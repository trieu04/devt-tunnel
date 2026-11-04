package askpass

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

func TestClassify(t *testing.T) {
	cases := map[string]Kind{
		"Enter passphrase for key '/home/me/.ssh/id_ed25519': ":                 KindPassphrase,
		"me@example.com's password: ":                                           KindPassword,
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? ": KindHostKey,
		"Verification code: ":                                                   KindOther,
	}
	for in, want := range cases {
		if got := Classify(in); got != want {
			t.Errorf("Classify(%q) = %s, want %s", in, got, want)
		}
	}
}

type recorder struct {
	mu      sync.Mutex
	prompts []Prompt
	answer  string
	err     error
}

func (r *recorder) ask(_ context.Context, p Prompt) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts = append(r.prompts, p)
	return r.answer, r.err
}

func setup(t *testing.T, rec *recorder) (*Broker, *secret.Memory) {
	t.Helper()
	store := &secret.Memory{}
	b, err := NewBroker("/path/helper", store, rec.ask)
	if err != nil {
		if os.IsPermission(err) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("local IPC is unavailable in this test environment: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b, store
}

func envValue(env []string, key string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v
		}
	}
	return ""
}

const passPrompt = "Enter passphrase for key 'k': "

func TestStoredPassphraseAnswersOnlyOnce(t *testing.T) {
	rec := &recorder{answer: "typed"}
	b, store := setup(t, rec)
	store.Set("key", "stored")
	env, release := b.Register("t1", "key")
	defer release()
	endpoint, token := envValue(env, EnvEndpoint), envValue(env, EnvToken)

	got, err := Ask(endpoint, token, passPrompt)
	if err != nil || got != "stored" {
		t.Fatalf("first: %q %v", got, err)
	}
	if len(rec.prompts) != 0 {
		t.Errorf("user was asked: %+v", rec.prompts)
	}
	// ssh asking again means the stored value was wrong: it must go to the user.
	got, err = Ask(endpoint, token, passPrompt)
	if err != nil || got != "typed" {
		t.Fatalf("second: %q %v", got, err)
	}
	if len(rec.prompts) != 1 || rec.prompts[0].TunnelID != "t1" || rec.prompts[0].Kind != KindPassphrase {
		t.Errorf("prompts %+v", rec.prompts)
	}
}

func TestStoredPassphraseNeverAnswersOtherPrompts(t *testing.T) {
	for _, prompt := range []string{
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? ",
		"me@host's password: ",
		"Verification code: ",
	} {
		rec := &recorder{answer: "from-user"}
		b, store := setup(t, rec)
		store.Set("key", "stored")
		env, release := b.Register("t1", "key")
		got, err := Ask(envValue(env, EnvEndpoint), envValue(env, EnvToken), prompt)
		release()
		if err != nil || got != "from-user" {
			t.Errorf("%q: got %q, %v", prompt, got, err)
		}
	}
}

func TestUnknownOrReleasedTokenRejected(t *testing.T) {
	rec := &recorder{answer: "x"}
	b, _ := setup(t, rec)
	env, release := b.Register("t1", "")
	endpoint, token := envValue(env, EnvEndpoint), envValue(env, EnvToken)
	if _, err := Ask(endpoint, "wrong", passPrompt); err == nil {
		t.Error("wrong token accepted")
	}
	release()
	if _, err := Ask(endpoint, token, passPrompt); err == nil {
		t.Error("released token accepted")
	}
	if len(rec.prompts) != 0 {
		t.Errorf("user was asked: %+v", rec.prompts)
	}
}

func TestUserCancelBecomesError(t *testing.T) {
	rec := &recorder{err: errors.New("cancelled")}
	b, _ := setup(t, rec)
	env, release := b.Register("t1", "")
	defer release()
	if _, err := Ask(envValue(env, EnvEndpoint), envValue(env, EnvToken), "code: "); err == nil || err.Error() != "cancelled" {
		t.Errorf("err %v", err)
	}
}

func TestEnvironmentForcesHelper(t *testing.T) {
	b, _ := setup(t, &recorder{})
	env, release := b.Register("t1", "")
	defer release()
	if envValue(env, "SSH_ASKPASS") != "/path/helper" || envValue(env, "SSH_ASKPASS_REQUIRE") != "force" {
		t.Errorf("env %v", env)
	}
}
