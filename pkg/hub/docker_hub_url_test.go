package hub

import (
	"testing"

	"github.com/elasticclaw/elasticclaw/pkg/types"
)

// A container reaches the host as host.docker.internal, so a loopback hub address
// has to be rewritten. When neither url nor public_url is configured — which is the
// case for the desktop app, since it passes --addr and never writes config — the
// listen address must be used instead. Returning an empty string here gave
// claw-bridge nothing to dial and every run died at "Connect" with only
// "provisioning timed out" reported.
func TestDockerClawHubURLFallsBackToTheListenAddress(t *testing.T) {
	got := dockerClawHubURL(&types.HubConfig{}, "127.0.0.1:8080")
	if got != "http://host.docker.internal:8080" {
		t.Errorf("with no configured url, got %q, want http://host.docker.internal:8080", got)
	}

	// ":8080" means every interface; the container still needs a host it can dial.
	if got := dockerClawHubURL(&types.HubConfig{}, "0.0.0.0:8090"); got != "http://host.docker.internal:8090" {
		t.Errorf("bare-port addr: got %q", got)
	}
	if got := dockerClawHubURL(&types.HubConfig{}, ":8090"); got != "http://host.docker.internal:8090" {
		t.Errorf("colon-prefixed addr: got %q", got)
	}
	if got := dockerClawHubURL(nil, "127.0.0.1:8080"); got != "http://host.docker.internal:8080" {
		t.Errorf("nil config: got %q", got)
	}
}

// A configured URL wins, and a public one is left alone.
func TestDockerClawHubURLPrefersConfigAndKeepsPublicHosts(t *testing.T) {
	cfg := &types.HubConfig{URL: "http://localhost:9000"}
	if got := dockerClawHubURL(cfg, "127.0.0.1:8080"); got != "http://host.docker.internal:9000" {
		t.Errorf("configured loopback url: got %q", got)
	}
	pub := &types.HubConfig{PublicURL: "https://hub.example.com"}
	if got := dockerClawHubURL(pub, "127.0.0.1:8080"); got != "https://hub.example.com" {
		t.Errorf("a reachable host must not be rewritten: got %q", got)
	}
}
