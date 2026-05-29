package registry

import (
	"strconv"
	"strings"
)

// valueSep is the field separator `reg query` uses between a value's
// name, type, and data: exactly four spaces (verified byte-exact via
// `od -c` against wine-11.0 — NOT a tab). It's also the indent value
// lines carry, distinguishing them from non-indented subkey paths.
const valueSep = "    "

// hiveAbbrev maps the registry-hive shorthands `reg` accepts on input to
// the full names it echoes back in its output, so the parser can match
// the header line even when the caller queried with an abbreviation.
var hiveAbbrev = map[string]string{
	"HKLM": "HKEY_LOCAL_MACHINE",
	"HKCU": "HKEY_CURRENT_USER",
	"HKCR": "HKEY_CLASSES_ROOT",
	"HKU":  "HKEY_USERS",
	"HKCC": "HKEY_CURRENT_CONFIG",
}

// parseRegQuery turns the stdout of a successful `wine reg query` into a
// QueryResult. queryKey is the key the caller asked for — needed because
// wine's output is structurally ambiguous without it (see below).
//
// Layout (after CRLF→LF), for a key that HAS values:
//
//	<blank line>
//	HKEY_…\Full\Key\Path                 (echoed header)
//	    <name>    <TYPE>    <data>        (4-space indent + 4-space gaps)
//	    …
//	<blank line>
//	HKEY_…\Full\Key\Path\Subkey          (subkeys, no indent)
//	…
//
// BUT for a value-less key wine OMITS the header entirely — the output
// is just the blank line followed by the subkey paths. So the echoed
// key path can't be found by line position; instead a non-indented line
// is the header iff it equals the queried key (resolving hive
// abbreviations), and every other non-indented line is a subkey. Values
// is always non-nil so the JSON shape stays stable.
func parseRegQuery(out []byte, queryKey string) QueryResult {
	text := strings.ReplaceAll(string(out), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	qr := QueryResult{Key: strings.TrimSpace(queryKey), Values: []Value{}}
	want := normalizeKey(queryKey)
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, valueSep) {
			if v, ok := parseValueLine(line); ok {
				qr.Values = append(qr.Values, v)
			}
			continue
		}
		// Non-indented, non-blank: the echoed header (== queried key) or
		// a subkey full path.
		if normalizeKey(line) == want {
			qr.Key = line // the canonical form wine echoed
			continue
		}
		qr.Subkeys = append(qr.Subkeys, line)
	}
	return qr
}

// normalizeKey upper-cases a key path, trims surrounding space and any
// trailing backslash, and expands a leading hive abbreviation, so two
// spellings of the same key compare equal.
func normalizeKey(k string) string {
	k = strings.ToUpper(strings.TrimSpace(k))
	k = strings.TrimRight(k, "\\")
	if i := strings.IndexByte(k, '\\'); i >= 0 {
		if full, ok := hiveAbbrev[k[:i]]; ok {
			return full + k[i:]
		}
	} else if full, ok := hiveAbbrev[k]; ok {
		return full
	}
	return k
}

// parseValueLine parses a single 4-space-indented value line into a
// Value. The leading indent is stripped; the first 4-space gap splits
// off the name, the second splits off the type, and the ENTIRE
// remainder is the data (preserving any internal 4-space runs an SZ
// value's data might contain). An empty-data REG_SZ (name + type +
// trailing gap + nothing) yields Data == "".
func parseValueLine(line string) (Value, bool) {
	rest := strings.TrimPrefix(line, valueSep)

	i := strings.Index(rest, valueSep)
	if i < 0 {
		return Value{}, false // no name/type separator — not a value line
	}
	name := rest[:i]
	afterName := rest[i+len(valueSep):]

	j := strings.Index(afterName, valueSep)
	if j < 0 {
		// name + type but no data gap — treat the remainder as the type.
		return Value{Name: name, Type: afterName}, true
	}
	typ := afterName[:j]
	data := afterName[j+len(valueSep):]
	if typ == "REG_DWORD" {
		data = normalizeDword(data)
	}
	return Value{Name: name, Type: typ, Data: data}, true
}

// normalizeDword converts Wine's REG_DWORD rendering ("0x" + lowercase
// hex, e.g. "0x7") to a decimal string ("7"). Anything that doesn't
// parse as 0x-prefixed hex is returned unchanged — graceful degradation
// rather than an error on an unexpected rendering.
func normalizeDword(s string) string {
	t := strings.TrimSpace(s)
	hex := strings.TrimPrefix(t, "0x")
	if hex == t {
		return s // no 0x prefix — leave as rendered
	}
	n, err := strconv.ParseUint(hex, 16, 64)
	if err != nil {
		return s
	}
	return strconv.FormatUint(n, 10)
}
