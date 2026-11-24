package persist

// Entry is one recorded session. It is a record of what existed, not a handle
// that can be used after a restart: only an h5 entry with a profile can be
// brought back, and only by opening the target again.
type Entry struct {
	ID      string `json:"id"`
	Backend string `json:"backend"` // android | ios | h5
	// Target is the h5 page URL (or file path); empty for the other backends.
	Target string `json:"target,omitempty"`
	// Profile is the persistent browser profile an h5 session used; empty when
	// it was ephemeral or the backend has no browser profile.
	Profile string `json:"profile,omitempty"`
	// Ephemeral records that the page kept its storage in memory only, so the
	// entry is a record with nothing to restore.
	Ephemeral bool `json:"ephemeral,omitempty"`
	// CreatedAt is RFC3339. The h5 session knows it; the in-process emulators
	// only track engines, so their entries omit it.
	CreatedAt string `json:"created_at,omitempty"`
	LastUsed  string `json:"last_used_at,omitempty"`
	// Reopen is the argument set that would bring an h5 session back.
	Reopen *Reopen `json:"reopen,omitempty"`
}

// Reopen is what a caller needs to restore an h5 session.
type Reopen struct {
	Target  string `json:"target,omitempty"`
	Profile string `json:"profile,omitempty"`
	Visible bool   `json:"visible,omitempty"`
}
