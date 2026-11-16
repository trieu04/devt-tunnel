package model

// Actor identifies the client that opened a tunnel. Session is a random,
// client-owned reconnect token, not an authorization boundary against the same
// OS user.
type Actor struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// TunnelOwner is presentation data. Holder is the client that opened the tunnel
// and has not closed it yet; it is nil for tunnels a person started.
type TunnelOwner struct {
	Pinned    bool   `json:"pinned"`
	Ephemeral bool   `json:"ephemeral"`
	Holder    *Actor `json:"holder,omitempty"`
}
