package apps

import "errors"

// ErrInstallerPathRequired is returned by Install when the supplied
// installer path is empty or whitespace-only.
var ErrInstallerPathRequired = errors.New("installer path is required")

// ErrExePathRequired is returned by Launch when the supplied
// executable path is empty or whitespace-only.
var ErrExePathRequired = errors.New("executable path is required")
