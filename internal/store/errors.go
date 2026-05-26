package store

import "errors"

// ErrBottleNotFound is returned by LoadBottle when no metadata exists
// for the requested id. Bridge handlers can wrap it with a -32602-style
// "no such bottle" error for the agent.
var ErrBottleNotFound = errors.New("bottle not found")
