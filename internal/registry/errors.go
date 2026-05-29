package registry

import "errors"

// ErrRegistryKeyRequired is returned by Query/Set when the supplied key
// path is empty or whitespace-only.
var ErrRegistryKeyRequired = errors.New("registry key is required")

// ErrRegistryKeyNotFound is returned by Query when `reg query` reports
// the key or value doesn't exist (exit 1 + the "Unable to find" marker).
// Distinct from an I/O / spawn failure so the MCP layer can map it to a
// 404-flavoured response.
var ErrRegistryKeyNotFound = errors.New("registry key or value not found")

// ErrRegistryTypeInvalid is returned by Set when the supplied value type
// isn't one of the recognized REG_* tokens.
var ErrRegistryTypeInvalid = errors.New("invalid registry value type")

// ErrRegistryWriteFailed is returned by Set when `reg add` exits
// non-zero. The wrapped error carries the captured output.
var ErrRegistryWriteFailed = errors.New("registry write failed")
