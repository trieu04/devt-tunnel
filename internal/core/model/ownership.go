package model

import "time"

// Actor identifies the client holding a lease. Session is a random, client-owned
// reconnect token, not an authorization boundary against the same OS user.
type Actor struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type Lease struct {
	ID         string     `json:"id"`
	TunnelID   string     `json:"tunnelId"`
	Holder     Actor      `json:"holder"`
	Session    string     `json:"session"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	StartedIt  bool       `json:"startedIt"`
	GraceUntil *time.Time `json:"graceUntil,omitempty"`
	Revoked    bool       `json:"revoked,omitempty"`
}

// TunnelOwner is presentation data; individual claims are available from lease.list.
type TunnelOwner struct {
	Pinned    bool       `json:"pinned"`
	Ephemeral bool       `json:"ephemeral"`
	Leases    int        `json:"leases"`
	Holder    *Actor     `json:"holder,omitempty"`
	Expiry    *time.Time `json:"expiry,omitempty"`
}
