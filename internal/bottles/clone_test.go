package bottles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeMarker drops a file into a bottle's prefix so a clone can be
// verified to have actually copied the prefix contents.
func writeMarker(t *testing.T, s interface {
	PrefixDir(string) (string, error)
}, id, rel, content string) {
	t.Helper()
	prefix, err := s.PrefixDir(id)
	if err != nil {
		t.Fatalf("PrefixDir: %v", err)
	}
	p := filepath.Join(prefix, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

func TestClone_HappyPath(t *testing.T) {
	m, s := newTestManager(t, fakeWineVersionBin(t))

	src, err := m.Create(context.Background(), "Diablo II")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	writeMarker(t, s, src.ID, "drive_c/marker.txt", "hello")

	dst, err := m.Clone(context.Background(), src.ID, "Diablo II (modded)")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if dst.ID == src.ID {
		t.Fatal("clone reused the source id")
	}
	if dst.Name != "Diablo II (modded)" {
		t.Errorf("Name = %q", dst.Name)
	}
	if dst.WineVersion != src.WineVersion {
		t.Errorf("WineVersion = %q, want inherited %q", dst.WineVersion, src.WineVersion)
	}

	// The prefix contents were copied.
	dstPrefix, _ := s.PrefixDir(dst.ID)
	got, err := os.ReadFile(filepath.Join(dstPrefix, "drive_c", "marker.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("clone marker = %q, err=%v", got, err)
	}
	// And there's no prefix-in-prefix nesting from cp onto an existing dir.
	if _, err := os.Stat(filepath.Join(dstPrefix, "prefix")); err == nil {
		t.Error("found nested prefix/prefix in the clone")
	}

	// The source is untouched and both show up in the list.
	if _, err := m.Get(src.ID); err != nil {
		t.Errorf("source gone after clone: %v", err)
	}
	bs, _ := m.List()
	if len(bs) != 2 {
		t.Errorf("List len = %d, want 2", len(bs))
	}
}

func TestClone_CopiesEnvOverridesIndependently(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	src, err := m.Create(context.Background(), "src")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.SetEnv(src.ID, "DXVK_HUD", "fps"); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}

	dst, err := m.Clone(context.Background(), src.ID, "dst")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if dst.EnvOverrides["DXVK_HUD"] != "fps" {
		t.Fatalf("clone env = %#v, want inherited DXVK_HUD=fps", dst.EnvOverrides)
	}

	// Editing the clone's env must not bleed into the source.
	if err := m.SetEnv(dst.ID, "DXVK_HUD", "full"); err != nil {
		t.Fatalf("SetEnv clone: %v", err)
	}
	srcEnv, _ := m.EnvOverrides(src.ID)
	if srcEnv["DXVK_HUD"] != "fps" {
		t.Errorf("source env mutated to %q — deep copy failed", srcEnv["DXVK_HUD"])
	}
}

func TestClone_EmptyEnvStaysNil(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	src, _ := m.Create(context.Background(), "src")
	dst, err := m.Clone(context.Background(), src.ID, "dst")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if dst.EnvOverrides != nil {
		t.Errorf("EnvOverrides = %#v, want nil for a source with none", dst.EnvOverrides)
	}
}

func TestClone_NameConflict(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	src, _ := m.Create(context.Background(), "src")
	if _, err := m.Clone(context.Background(), src.ID, "SRC"); !errors.Is(err, ErrBottleNameConflict) {
		t.Fatalf("err = %v, want ErrBottleNameConflict (case-insensitive)", err)
	}
}

func TestClone_EmptyName(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	src, _ := m.Create(context.Background(), "src")
	if _, err := m.Clone(context.Background(), src.ID, "   "); !errors.Is(err, ErrBottleNameRequired) {
		t.Fatalf("err = %v, want ErrBottleNameRequired", err)
	}
}

func TestClone_SourceNotFound(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	_, err := m.Clone(context.Background(), "11111111-1111-1111-1111-111111111111", "dst")
	if !errors.Is(err, ErrBottleNotFound) {
		t.Fatalf("err = %v, want ErrBottleNotFound", err)
	}
	// A failed clone must not leave a phantom bottle behind.
	if bs, _ := m.List(); len(bs) != 0 {
		t.Errorf("List len = %d, want 0 after failed clone", len(bs))
	}
}
