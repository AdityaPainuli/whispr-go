package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingFileWritesDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg != Default() {
		t.Fatalf("cfg = %+v, want defaults", cfg)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("defaults file not written for discoverability")
	}
	// The written file must round-trip.
	if again, err := Load(p); err != nil || again != Default() {
		t.Fatalf("round-trip: %+v, %v", again, err)
	}
}

func TestPartialFileKeepsDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"pause_seconds": 2.0}`), 0o644)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PauseSeconds != 2.0 {
		t.Fatalf("PauseSeconds = %v, want 2.0", cfg.PauseSeconds)
	}
	if cfg.Port != 8181 || !cfg.Refine || cfg.Corrections != "auto" {
		t.Fatalf("unset fields lost defaults: %+v", cfg)
	}
}

func TestInvalidValuesFailLoudly(t *testing.T) {
	cases := []string{
		`{"port": -1}`,
		`{"pause_seconds": 0.05}`,
		`{"corrections": "maybe"}`,
		`{not json`,
	}
	for _, c := range cases {
		p := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(p, []byte(c), 0o644)
		if _, err := Load(p); err == nil {
			t.Errorf("Load(%s): want error, got nil", c)
		}
	}
}
