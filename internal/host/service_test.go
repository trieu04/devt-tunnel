package host

import (
	"testing"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestNewDraftUsesConsistentLocalServiceURL(t *testing.T) {
	service := &Service{}
	ngrok := service.NewDraft(model.TypeNgrok)
	cloudflared := service.NewDraft(model.TypeCloudflared)
	const want = "http://127.0.0.1:3000"
	if ngrok.Ngrok == nil || ngrok.Ngrok.Addr != want {
		t.Fatalf("ngrok address = %#v, want %q", ngrok.Ngrok, want)
	}
	if cloudflared.Cloudflared == nil || cloudflared.Cloudflared.URL != want {
		t.Fatalf("cloudflared URL = %#v, want %q", cloudflared.Cloudflared, want)
	}
}
