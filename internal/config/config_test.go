package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveWritesPrivateConfigFileAndDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aidur", "config.json")
	t.Setenv("AIDUR_CONFIG", path)
	cfg := Config{Model: "gpt-5.4-mini", Thinking: "medium", Instructions: "be brief", OpenCodeZenAPIKey: "sk-test"}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config dir mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("config file mode = %o, want 600", got)
	}
	loaded := Load()
	if loaded.Instructions != cfg.Instructions || loaded.OpenCodeZenAPIKey != cfg.OpenCodeZenAPIKey {
		t.Fatalf("Load() = %+v, want instructions and API key preserved", loaded)
	}
}

func TestValidThinkingIncludesOff(t *testing.T) {
	for _, value := range []string{"off", "low", "medium", "high"} {
		if !ValidThinking(value) {
			t.Fatalf("ValidThinking(%q) = false, want true", value)
		}
	}
	if ValidThinking("nope") {
		t.Fatalf("ValidThinking(nope) = true, want false")
	}
}
