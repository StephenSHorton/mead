package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTempStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestOpen_DefaultPathResolves(t *testing.T) {
	// We pass an empty root, expecting Open to default to a path under
	// the user's home. Use a temp HOME so we don't write into the
	// developer's real Library.
	t.Setenv("HOME", t.TempDir())
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	if s.Root() == "" {
		t.Error("Root() returned empty")
	}
	// The bottles/ subdir should have been created.
	if _, err := os.Stat(filepath.Join(s.Root(), "bottles")); err != nil {
		t.Errorf("bottles/ dir not created: %v", err)
	}
}

func TestOpen_RejectsFileAsRoot(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Open will try to MkdirAll(path/bottles) which fails because path
	// is a file. That's a fine failure; we just want to ensure we
	// don't somehow corrupt the file.
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected error when root is a file")
	}
	// Confirm we didn't overwrite the existing file.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if string(data) != "x" {
		t.Errorf("file mutated: got %q want %q", data, "x")
	}
}

func TestSaveAndLoadBottle_RoundTrip(t *testing.T) {
	s := newTempStore(t)
	b := &Bottle{
		ID:          "11111111-1111-1111-1111-111111111111",
		Name:        "Diablo II",
		CreatedAt:   time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		WineVersion: "wine-9.0",
		EnvOverrides: map[string]string{"DXVK_HUD": "fps"},
	}
	if err := s.SaveBottle(b); err != nil {
		t.Fatalf("SaveBottle: %v", err)
	}
	got, err := s.LoadBottle(b.ID)
	if err != nil {
		t.Fatalf("LoadBottle: %v", err)
	}
	if got.ID != b.ID || got.Name != b.Name || got.WineVersion != b.WineVersion {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, b)
	}
	if got.EnvOverrides["DXVK_HUD"] != "fps" {
		t.Errorf("env override lost: got %+v", got.EnvOverrides)
	}
	if !got.CreatedAt.Equal(b.CreatedAt) {
		t.Errorf("created_at lost: got %v want %v", got.CreatedAt, b.CreatedAt)
	}
}

func TestLoadBottle_NotFound(t *testing.T) {
	s := newTempStore(t)
	_, err := s.LoadBottle("22222222-2222-2222-2222-222222222222")
	if !errors.Is(err, ErrBottleNotFound) {
		t.Errorf("expected ErrBottleNotFound, got %v", err)
	}
}

func TestLoadBottles_OrderedByCreatedAt(t *testing.T) {
	s := newTempStore(t)
	ids := []string{
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		"00000000-0000-0000-0000-000000000003",
	}
	times := []time.Time{
		time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	for i := range ids {
		if err := s.SaveBottle(&Bottle{ID: ids[i], Name: "b" + ids[i][35:], CreatedAt: times[i]}); err != nil {
			t.Fatalf("SaveBottle: %v", err)
		}
	}
	got, err := s.LoadBottles()
	if err != nil {
		t.Fatalf("LoadBottles: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d bottles, want 3", len(got))
	}
	// Sorted by createdAt: ids[1] (Jan 1), ids[2] (Jan 2), ids[0] (Jan 3).
	wantOrder := []string{ids[1], ids[2], ids[0]}
	for i, b := range got {
		if b.ID != wantOrder[i] {
			t.Errorf("bottle[%d].ID = %s; want %s", i, b.ID, wantOrder[i])
		}
	}
}

func TestLoadBottles_SkipsInvalidDirs(t *testing.T) {
	s := newTempStore(t)
	// Drop a junk directory + a UUID-shaped directory with no metadata.
	if err := os.MkdirAll(filepath.Join(s.Root(), "bottles", "not-a-uuid"), 0o755); err != nil {
		t.Fatalf("mkdir junk: %v", err)
	}
	halfBaked := "33333333-3333-3333-3333-333333333333"
	if err := os.MkdirAll(filepath.Join(s.Root(), "bottles", halfBaked, "prefix"), 0o755); err != nil {
		t.Fatalf("mkdir half-baked: %v", err)
	}
	// And a real bottle.
	real := &Bottle{ID: "44444444-4444-4444-4444-444444444444", Name: "real", CreatedAt: time.Now().UTC()}
	if err := s.SaveBottle(real); err != nil {
		t.Fatalf("SaveBottle: %v", err)
	}
	got, err := s.LoadBottles()
	if err != nil {
		t.Fatalf("LoadBottles: %v", err)
	}
	if len(got) != 1 || got[0].ID != real.ID {
		t.Errorf("expected only the real bottle, got %d entries", len(got))
	}
}

func TestDeleteBottle_Idempotent(t *testing.T) {
	s := newTempStore(t)
	b := &Bottle{ID: "55555555-5555-5555-5555-555555555555", Name: "x", CreatedAt: time.Now().UTC()}
	if err := s.SaveBottle(b); err != nil {
		t.Fatalf("SaveBottle: %v", err)
	}
	if err := s.DeleteBottle(b.ID); err != nil {
		t.Fatalf("Delete #1: %v", err)
	}
	if err := s.DeleteBottle(b.ID); err != nil {
		t.Errorf("Delete #2 should be idempotent, got %v", err)
	}
}

func TestNameTaken(t *testing.T) {
	s := newTempStore(t)
	if err := s.SaveBottle(&Bottle{
		ID: "66666666-6666-6666-6666-666666666666", Name: "  Foo Bar  ",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveBottle: %v", err)
	}
	for _, c := range []struct {
		name string
		want bool
	}{
		{"Foo Bar", true},
		{"foo bar", true},        // case-insensitive
		{" foo bar ", true},      // whitespace-trimmed
		{"FooBar", false},        // distinct enough
		{"Something else", false},
	} {
		got, err := s.NameTaken(c.name)
		if err != nil {
			t.Fatalf("NameTaken(%q): %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("NameTaken(%q) = %v; want %v", c.name, got, c.want)
		}
	}
}

func TestUUIDIsh_RejectsPathTraversal(t *testing.T) {
	for _, s := range []string{
		"",
		"../etc/passwd",
		"hello",
		"11111111-1111-1111-1111-11111111111",  // too short
		"11111111-1111-1111-1111-1111111111111", // too long
		"GGGGGGGG-GGGG-GGGG-GGGG-GGGGGGGGGGGG",  // non-hex
		"11111111_1111_1111_1111_111111111111",  // wrong separator
	} {
		if UUIDIsh(s) {
			t.Errorf("UUIDIsh(%q) = true; want false", s)
		}
	}
	for _, s := range []string{
		"11111111-1111-1111-1111-111111111111",
		"deadbeef-cafe-1234-5678-90abcdef0123",
	} {
		if !UUIDIsh(s) {
			t.Errorf("UUIDIsh(%q) = false; want true", s)
		}
	}
}

func TestBottleDir_PathTraversalRejected(t *testing.T) {
	s := newTempStore(t)
	for _, id := range []string{
		"../escape",
		"good-but-fake",
		"",
	} {
		if _, err := s.BottleDir(id); err == nil {
			t.Errorf("BottleDir(%q): expected error, got nil", id)
		}
	}
}
