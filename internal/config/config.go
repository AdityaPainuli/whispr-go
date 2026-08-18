// Package config loads user settings from a JSON file next to the models
// directory. The file is written with defaults on first run so people can
// discover what's tunable by opening it — there's no settings UI yet.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	// Port for the local cleanup LLM server.
	Port int `json:"port"`
	// Refine toggles the cleanup LLM entirely. Off = raw ASR text, which
	// already has punctuation.
	Refine bool `json:"refine"`
	// Corrections: "auto" gates on RAM (3B model needs 16GB), "on"/"off"
	// force it. Forcing "on" with 8GB will starve the speech decoder —
	// measured, not theoretical. You were warned.
	Corrections string `json:"corrections"`
	// PauseSeconds of silence that closes a sentence while you talk.
	// Lower = snappier cleanup, more risk that a mid-sentence hesitation
	// splits the sentence. Raise it if you get "Send the report to. John."
	PauseSeconds float64 `json:"pause_seconds"`
	// IdleUnloadMinutes: stop the cleanup LLM after this long without a
	// dictation and reload it on the next one (first dictation after a
	// long idle may paste raw while it reloads). 0 = keep it resident.
	IdleUnloadMinutes int `json:"idle_unload_minutes"`
}

func Default() Config {
	return Config{
		Port:              8181,
		Refine:            true,
		Corrections:       "auto",
		PauseSeconds:      1.2,
		IdleUnloadMinutes: 15,
	}
}

// Path returns the config file location: alongside the whispr-go data dir.
func Path(dataDir string) string {
	return filepath.Join(dataDir, "config.json")
}

// Load reads cfgPath, filling gaps with defaults. A missing file is
// created with the defaults so it's discoverable. A malformed file is an
// error — silently ignoring a typo'd config is worse than failing loudly.
func Load(cfgPath string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(cfgPath)
	if os.IsNotExist(err) {
		if b, err := json.MarshalIndent(cfg, "", "  "); err == nil {
			os.MkdirAll(filepath.Dir(cfgPath), 0o755)
			os.WriteFile(cfgPath, b, 0o644)
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("config: read %s: %w", cfgPath, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", cfgPath, err)
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return cfg, fmt.Errorf("config: port %d out of range", cfg.Port)
	}
	if cfg.PauseSeconds < 0.3 || cfg.PauseSeconds > 10 {
		return cfg, fmt.Errorf("config: pause_seconds %.1f out of range (0.3-10)", cfg.PauseSeconds)
	}
	switch cfg.Corrections {
	case "auto", "on", "off":
	default:
		return cfg, fmt.Errorf("config: corrections must be auto, on, or off (got %q)", cfg.Corrections)
	}
	return cfg, nil
}
