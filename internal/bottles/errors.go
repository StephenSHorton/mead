package bottles

import "errors"

// ErrBottleNotFound is returned by Get/Delete when the supplied id
// isn't registered. Distinct from a real I/O error so the MCP handler
// can map it to a 404-flavoured response.
var ErrBottleNotFound = errors.New("bottle not found")

// ErrBottleNameConflict is returned by Create when the supplied display
// name is already in use. (IDs are server-generated so they never
// collide; names are user-supplied so they can.)
var ErrBottleNameConflict = errors.New("bottle name already in use")

// ErrBottleNameRequired is returned by Create when the supplied name
// is empty or whitespace-only.
var ErrBottleNameRequired = errors.New("bottle name is required")

// ErrEnvKeyRequired is returned by SetEnv when the supplied env-var
// name is empty. (Empty values are valid — they delete the key.)
var ErrEnvKeyRequired = errors.New("env key is required")
