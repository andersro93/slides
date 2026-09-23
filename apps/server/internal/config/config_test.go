package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DEV", "")
	t.Setenv("RELOAD_INTERVAL", "")
	c, err := Load()
	if err != nil || c.Port != 3000 || c.Dev || c.ReloadInterval != 5*time.Second {
		t.Fatalf("defaults = %+v, %v", c, err)
	}

	t.Setenv("DEV", "1")
	if c, _ := Load(); !c.Dev || c.ReloadInterval != time.Second {
		t.Fatalf("dev = %+v", c)
	}
	t.Setenv("RELOAD_INTERVAL", "30s")
	if c, _ := Load(); c.ReloadInterval != 30*time.Second {
		t.Fatalf("interval = %v", c.ReloadInterval)
	}

	for env, bad := range map[string]string{"PORT": "0", "DEV": "maybe", "RELOAD_INTERVAL": "1ms"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, bad)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), env) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for decksDir, want := range map[string]string{
		"":                           "DECKS_DIR is not set",
		filepath.Join(dir, "absent"): "mounted",
		file:                         "not a directory",
		dir:                          "",
	} {
		err := Config{DecksDir: decksDir}.Validate()
		if (want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), want)) {
			t.Errorf("Validate(%q) = %v, want %q", decksDir, err, want)
		}
	}
}
