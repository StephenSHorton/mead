package store

import "errors"

// errNotImplemented is the sentinel every stub returns until its body
// lands. Centralized so test code can `errors.Is(err, ErrNotImplemented)`
// against an exported version without touching every stub.
var errNotImplemented = errors.New("not implemented yet")

// ErrNotImplemented is the exported alias for the sentinel above, for
// callers that want to detect "this method is still a stub" explicitly
// (e.g. the UI's feature-gating, or integration tests).
var ErrNotImplemented = errNotImplemented
