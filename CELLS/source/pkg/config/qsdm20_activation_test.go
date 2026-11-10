package config

import (
	"os"
	"path/filepath"
	"testing"
)

// CELLS-20 tokens are a consensus rule with their own activation height. The
// default must stay zero so shipping the binary changes nothing.
func TestCELLS20ActivationHeight_DefaultTOMLYAMLAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CELLS.toml")
	if err := os.WriteFile(path, []byte("[consensus]\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("CONFIG_FILE", path)
	t.Setenv("CELLS_CELLS20_ACTIVATION_HEIGHT", "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.CELLS20ActivationHeight != 0 {
		t.Fatalf("default must be 0 (off), got %d", cfg.CELLS20ActivationHeight)
	}

	if err := os.WriteFile(path, []byte("[consensus]\nCELLS20_activation_height = 910000\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if cfg, err = LoadConfig(); err != nil || cfg.CELLS20ActivationHeight != 910_000 {
		t.Fatalf("toml key not read: %d %v", cfg.CELLS20ActivationHeight, err)
	}

	yamlPath := filepath.Join(dir, "CELLS.yaml")
	if err := os.WriteFile(yamlPath, []byte("consensus:\n  CELLS20_activation_height: 910001\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("CONFIG_FILE", yamlPath)
	if cfg, err = LoadConfig(); err != nil || cfg.CELLS20ActivationHeight != 910_001 {
		t.Fatalf("yaml key not read: %d %v", cfg.CELLS20ActivationHeight, err)
	}

	t.Setenv("CELLS_CELLS20_ACTIVATION_HEIGHT", "910002")
	if cfg, err = LoadConfig(); err != nil || cfg.CELLS20ActivationHeight != 910_002 {
		t.Fatalf("env override not applied: %d %v", cfg.CELLS20ActivationHeight, err)
	}
}
