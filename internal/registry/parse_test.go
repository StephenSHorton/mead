package registry

import (
	"reflect"
	"testing"
)

// crlf joins lines with the CRLF endings wine's `reg` actually emits, so
// these fixtures are byte-for-byte what the parser sees in production.
func crlf(lines ...string) []byte {
	out := ""
	for _, l := range lines {
		out += l + "\r\n"
	}
	return []byte(out)
}

func TestParseRegQuery_ValuesAndDword(t *testing.T) {
	// Exactly the layout captured from `reg query …\MeadTest` in the
	// probe: leading blank, key header, two 4-space value lines, blank.
	out := crlf(
		"",
		`HKEY_CURRENT_USER\Software\MeadTest`,
		"    Count    REG_DWORD    0x7",
		"    Greeting    REG_SZ    hello",
		"",
	)
	got := parseRegQuery(out, `HKEY_CURRENT_USER\Software\MeadTest`)

	if got.Key != `HKEY_CURRENT_USER\Software\MeadTest` {
		t.Errorf("Key = %q", got.Key)
	}
	want := []Value{
		{Name: "Count", Type: "REG_DWORD", Data: "7"}, // 0x7 normalized to decimal
		{Name: "Greeting", Type: "REG_SZ", Data: "hello"},
	}
	if !reflect.DeepEqual(got.Values, want) {
		t.Errorf("Values = %#v, want %#v", got.Values, want)
	}
	if len(got.Subkeys) != 0 {
		t.Errorf("Subkeys = %v, want none", got.Subkeys)
	}
}

func TestParseRegQuery_ValuesThenSubkeys(t *testing.T) {
	out := crlf(
		"",
		`HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion`,
		`    CommonFilesDir    REG_SZ    C:\Program Files\Common Files`,
		"    ProgramFilesPath    REG_EXPAND_SZ    %ProgramFiles%",
		"",
		`HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Setup`,
		`HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Uninstall`,
	)
	got := parseRegQuery(out, `HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion`)

	// Data containing a drive path with spaces (but no 4-space run) must
	// survive intact, including its REG_SZ value.
	if len(got.Values) != 2 || got.Values[0].Data != `C:\Program Files\Common Files` {
		t.Fatalf("Values = %#v", got.Values)
	}
	if got.Values[1].Type != "REG_EXPAND_SZ" || got.Values[1].Data != "%ProgramFiles%" {
		t.Errorf("expand value = %#v", got.Values[1])
	}
	wantSub := []string{
		`HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Setup`,
		`HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Uninstall`,
	}
	if !reflect.DeepEqual(got.Subkeys, wantSub) {
		t.Errorf("Subkeys = %v, want %v", got.Subkeys, wantSub)
	}
}

func TestParseRegQuery_SubkeysOnlyNoHeader(t *testing.T) {
	// A value-less key: wine omits the echoed header line entirely, so
	// the FIRST non-blank line is already a subkey. The parser must not
	// mistake it for the key path.
	out := crlf(
		"",
		`HKEY_CURRENT_USER\Software\Wine\AppDefaults`,
		`HKEY_CURRENT_USER\Software\Wine\Debug`,
		`HKEY_CURRENT_USER\Software\Wine\DllOverrides`,
	)
	got := parseRegQuery(out, `HKEY_CURRENT_USER\Software\Wine`)

	if got.Key != `HKEY_CURRENT_USER\Software\Wine` {
		t.Errorf("Key = %q, want the queried key (header was omitted)", got.Key)
	}
	if len(got.Values) != 0 {
		t.Errorf("Values = %#v, want none", got.Values)
	}
	if len(got.Subkeys) != 3 {
		t.Errorf("Subkeys = %v, want all 3 (none swallowed as the key)", got.Subkeys)
	}
}

func TestParseRegQuery_HiveAbbreviationMatchesHeader(t *testing.T) {
	// Caller queried with HKCU; wine echoes the full HKEY_CURRENT_USER.
	// The abbreviation must still resolve so the header isn't mistaken
	// for a subkey.
	out := crlf(
		"",
		`HKEY_CURRENT_USER\Software\MeadTest`,
		"    Greeting    REG_SZ    hi",
	)
	got := parseRegQuery(out, `HKCU\Software\MeadTest`)
	if got.Key != `HKEY_CURRENT_USER\Software\MeadTest` {
		t.Errorf("Key = %q, want resolved header", got.Key)
	}
	if len(got.Subkeys) != 0 {
		t.Errorf("Subkeys = %v, header leaked as a subkey", got.Subkeys)
	}
}

func TestParseRegQuery_EmptySZ(t *testing.T) {
	// Empty REG_SZ renders as name + type + trailing gap + nothing.
	out := crlf(
		"",
		`HKEY_CURRENT_USER\Software\MeadTest`,
		"    RegisteredOwner    REG_SZ    ",
	)
	got := parseRegQuery(out, `HKEY_CURRENT_USER\Software\MeadTest`)
	if len(got.Values) != 1 || got.Values[0].Name != "RegisteredOwner" || got.Values[0].Data != "" {
		t.Errorf("Values = %#v, want one empty-data REG_SZ", got.Values)
	}
}

func TestNormalizeDword(t *testing.T) {
	cases := map[string]string{
		"0x7":        "7",
		"0x0":        "0",
		"0xffffffff": "4294967295",
		"7":          "7",        // no prefix → unchanged
		"0xnothex":   "0xnothex", // unparseable → unchanged
	}
	for in, want := range cases {
		if got := normalizeDword(in); got != want {
			t.Errorf("normalizeDword(%q) = %q, want %q", in, got, want)
		}
	}
}
