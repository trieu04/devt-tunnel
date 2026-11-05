package command

import (
	"encoding/hex"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func fingerprintSpec() Spec {
	return Spec{
		Tool: "ssh", Args: []string{"-N", "-L", "127.0.0.1:20000:db:5432", "staging"},
		Dir: "/work", SecretEnv: map[string]string{"TOKEN": "token-name", "PASSWORD": "password-name"},
		Askpass: true, Passphrase: "passphrase-name",
	}
}

func TestFingerprintStableAndIndependentOfMapOrder(t *testing.T) {
	spec := fingerprintSpec()
	want := spec.Fingerprint()
	if decoded, err := hex.DecodeString(want); err != nil || len(decoded) != 32 {
		t.Fatalf("not a SHA-256 hash: %q, %v", want, err)
	}
	for range 100 {
		other := fingerprintSpec()
		other.SecretEnv = make(map[string]string)
		other.SecretEnv["PASSWORD"] = "password-name"
		other.SecretEnv["TOKEN"] = "token-name"
		if other.Fingerprint() != want || spec.Fingerprint() != want {
			t.Fatal("fingerprint is not stable across calls/map insertion order")
		}
	}
	other := fingerprintSpec()
	other.SecretEnv = nil
	empty := other
	empty.SecretEnv = map[string]string{}
	if other.Fingerprint() != empty.Fingerprint() {
		t.Fatal("nil and empty environments should be equivalent")
	}
	other.Args = nil
	empty.Args = []string{}
	if other.Fingerprint() != empty.Fingerprint() {
		t.Fatal("nil and empty argument lists should be equivalent")
	}
}

func TestFingerprintChangesForExecutionInputs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Spec)
	}{
		{"tool", func(s *Spec) { s.Tool = "/different/ssh" }},
		{"argument", func(s *Spec) { s.Args[3] = "production" }},
		{"argument_order", func(s *Spec) { s.Args[0], s.Args[1] = s.Args[1], s.Args[0] }},
		{"argument_count", func(s *Spec) { s.Args = append(s.Args, "") }},
		{"dir", func(s *Spec) { s.Dir = "/different/work" }},
		{"environment_name", func(s *Spec) { delete(s.SecretEnv, "TOKEN"); s.SecretEnv["OTHER_TOKEN"] = "token-name" }},
		{"secret_reference", func(s *Spec) { s.SecretEnv["TOKEN"] = "different-token-name" }},
		{"environment_mapping", func(s *Spec) {
			s.SecretEnv["TOKEN"], s.SecretEnv["PASSWORD"] = s.SecretEnv["PASSWORD"], s.SecretEnv["TOKEN"]
		}},
		{"environment_removed", func(s *Spec) { delete(s.SecretEnv, "TOKEN") }},
		{"passphrase_reference", func(s *Spec) { s.Passphrase = "different-passphrase-name" }},
		{"askpass_routing", func(s *Spec) { s.Askpass = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := fingerprintSpec()
			after := fingerprintSpec()
			tc.change(&after)
			if before.Fingerprint() == after.Fingerprint() {
				t.Fatal("execution input change reused an approval fingerprint")
			}
		})
	}
}

func TestFingerprintFramesFieldsAndArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b Spec
	}{
		{"argv_boundaries", Spec{Tool: "tool", Args: []string{"a b", "c"}}, Spec{Tool: "tool", Args: []string{"a", "b c"}}},
		{"tool_boundary", Spec{Tool: "tool a", Args: []string{"b"}}, Spec{Tool: "tool", Args: []string{"a b"}}},
		{"env_secret_boundary", Spec{SecretEnv: map[string]string{"TOKEN=A": "B"}}, Spec{SecretEnv: map[string]string{"TOKEN": "A=B"}}},
		{"nul_boundary", Spec{Tool: "a\x00b", Args: []string{"c"}}, Spec{Tool: "a", Args: []string{"b\x00c"}}},
		{"quotes", Spec{Tool: "tool", Args: []string{`a" "b`}}, Spec{Tool: "tool", Args: []string{"a", "b"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.a.Fingerprint() == tc.b.Fingerprint() {
				t.Fatal("distinct input tuples collided")
			}
		})
	}
}

func TestFingerprintExcludesReadinessAndDisplayMetadata(t *testing.T) {
	spec := fingerprintSpec()
	other := fingerprintSpec()
	other.Ready = Ready{Ports: []string{"localhost:20000"}, LogPattern: regexp.MustCompile("ready"), AliveFor: time.Minute}
	other.CommandLine = "display only"
	if spec.Fingerprint() != other.Fingerprint() {
		t.Fatal("readiness/display changes should not change the spawned process identity")
	}
}

func TestFingerprintBuildUsesSecretNamesNotValues(t *testing.T) {
	// The spec has no secret-value input or store. Only the reference name
	// enters it, so rotating a value outside Build cannot change the hash.
	tunnel := model.Tunnel{Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "3000", Secret: "ngrok-personal"}}
	before, err := Build(tunnel, Options{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := Build(tunnel, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if before.Fingerprint() != after.Fingerprint() {
		t.Fatal("same reference is not stable")
	}
	tunnel.Ngrok.Secret = "ngrok-work"
	after, err = Build(tunnel, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Args, after.Args) || before.Fingerprint() == after.Fingerprint() {
		t.Fatal("changing a secret reference must invalidate approval even with identical argv")
	}
}

func TestFingerprintDoesNotMutateSpec(t *testing.T) {
	spec := fingerprintSpec()
	want := spec
	want.Args, want.SecretEnv = slices.Clone(spec.Args), maps.Clone(spec.SecretEnv)
	spec.Fingerprint()
	if !reflect.DeepEqual(spec, want) {
		t.Fatal("fingerprinting mutated spec")
	}
}
