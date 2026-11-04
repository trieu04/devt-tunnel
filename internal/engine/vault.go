package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

var secretName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func (s *Engine) ListSecrets() []api.SecretView {
	byName := map[string]*api.SecretView{}
	lastUsed := map[string]time.Time{}
	var entries []secret.Entry
	if err := s.withSecretIndex(func(index secretIndexStore) error {
		entries = index.List()
		return nil
	}); err != nil {
		return []api.SecretView{}
	}
	for _, entry := range entries {
		view := &api.SecretView{Name: entry.Name, Kind: entry.Kind, State: "missing"}
		byName[entry.Name] = view
		lastUsed[entry.Name] = entry.LastUsed
	}
	if s.lifetimeError() != nil {
		return []api.SecretView{}
	}
	for _, tunnel := range s.manager.Config().Tunnels {
		for _, name := range tunnel.Secrets() {
			view := byName[name]
			if view == nil {
				view = &api.SecretView{Name: name, Kind: inferKind(tunnel, name), State: "missing"}
				byName[name] = view
			}
			view.UsedBy = append(view.UsedBy, tunnel.Name)
		}
	}
	out := make([]api.SecretView, 0, len(byName))
	for name, view := range byName {
		if s.lifetimeError() != nil {
			return []api.SecretView{}
		}
		if _, err := secret.Has(s.secrets, name); err == nil {
			view.State = "present"
		} else if !errors.Is(err, secret.ErrNotFound) {
			view.State = "unavailable"
		}
		if used := lastUsed[name]; !used.IsZero() {
			view.LastUsed = used.Format(time.RFC3339)
		}
		sort.Strings(view.UsedBy)
		out = append(out, *view)
	}
	if s.lifetimeError() != nil {
		return []api.SecretView{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func inferKind(t model.Tunnel, name string) secret.Kind {
	switch t.Type {
	case model.TypeSSH:
		return secret.KindSSHPassphrase
	case model.TypeNgrok:
		return secret.KindNgrok
	case model.TypeCloudflared:
		return secret.KindCloudflared
	default:
		return secret.KindEnv
	}
}

func (s *Engine) DeleteSecret(name string) error {
	if err := s.secrets.Delete(name); err != nil {
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("secret deletion may have completed, but shutdown canceled confirmation: %w", err)
		}
		return err
	}
	if err := s.withSecretIndex(func(index secretIndexStore) error { return index.Delete(name) }); err != nil {
		return fmt.Errorf("secret was deleted, but its metadata was not updated: %w", err)
	}
	if err := s.applyWhileAlive(s.notify); err != nil {
		return fmt.Errorf("secret was deleted, but shutdown canceled its refresh: %w", err)
	}
	return nil
}

func (s *Engine) RenameSecret(from, to string) error {
	if !secretName.MatchString(to) {
		return errors.New("invalid secret name")
	}
	value, err := s.secrets.Get(from)
	if err != nil {
		return err
	}
	if err := s.secrets.Set(to, value); err != nil {
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("secret copy may have completed, but shutdown canceled confirmation: %w", err)
		}
		return err
	}
	err = s.updateConfig(func(cfg *model.Config) error {
		for n := range cfg.Tunnels {
			replaceSecretRefs(&cfg.Tunnels[n], from, to)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("secret was copied, but shutdown interrupted the reference update; both names may remain: %w", err)
		}
		_ = s.secrets.Delete(to)
		return err
	}
	if err := s.secrets.Delete(from); err != nil {
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("references were renamed, but deletion of the old credential may have completed without confirmation: %w", err)
		}
		return err
	}
	if err := s.withSecretIndex(func(index secretIndexStore) error { return index.Rename(from, to) }); err != nil {
		return fmt.Errorf("credentials and references were renamed, but metadata was not updated: %w", err)
	}
	return nil
}

func replaceSecretRefs(t *model.Tunnel, from, to string) {
	if t.SSH != nil && t.SSH.PassphraseSecret == from {
		t.SSH.PassphraseSecret = to
	}
	if t.Ngrok != nil && t.Ngrok.Secret == from {
		t.Ngrok.Secret = to
	}
	if t.Cloudflared != nil && t.Cloudflared.Secret == from {
		t.Cloudflared.Secret = to
	}
	if t.Command != nil {
		for env, name := range t.Command.SecretEnvs {
			if name == from {
				t.Command.SecretEnvs[env] = to
			}
		}
	}
}
