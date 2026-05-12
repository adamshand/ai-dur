package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const DefaultModel = "gpt-5.4-mini"
const DefaultThinking = "medium"
const DefaultAgentName = "aidur"

type Config struct {
	Model     string `json:"model"`
	Thinking  string `json:"thinking"`
	AgentName string `json:"agent_name"`
}

func Path() string {
	if override := os.Getenv("AIDUR_CONFIG"); override != "" {
		return expandHome(override)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "aidur", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", "aidur", "config.json")
	}
	return filepath.Join(home, ".config", "aidur", "config.json")
}

func Load() Config {
	data, err := os.ReadFile(Path())
	if err != nil {
		return Config{}
	}
	var cfg Config
	if json.Unmarshal(data, &cfg) != nil {
		return Config{}
	}
	return cfg
}

func Save(cfg Config) error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func EffectiveModel(cfg Config) (model, source string) {
	if env := os.Getenv("AIDUR_MODEL"); env != "" {
		return env, "AIDUR_MODEL"
	}
	if cfg.Model != "" {
		return cfg.Model, "config"
	}
	return DefaultModel, "default"
}

func EffectiveThinking(cfg Config) (thinking, source string) {
	if env := os.Getenv("AIDUR_THINKING"); env != "" {
		return env, "AIDUR_THINKING"
	}
	if cfg.Thinking != "" {
		return cfg.Thinking, "config"
	}
	return DefaultThinking, "default"
}

func EffectiveAgentName(cfg Config) (name, source string) {
	if env, ok := NormalizeAgentName(os.Getenv("AIDUR_AGENT_NAME")); ok {
		return env, "AIDUR_AGENT_NAME"
	}
	if name, ok := NormalizeAgentName(cfg.AgentName); ok {
		return name, "config"
	}
	return DefaultAgentName, "default"
}

func ValidThinking(value string) bool {
	switch value {
	case "off", "low", "medium", "high":
		return true
	default:
		return false
	}
}

func NormalizeAgentName(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 32 {
		return "", false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '>' || r == ':' {
			return "", false
		}
	}
	return value, true
}

func expandHome(path string) string {
	if len(path) >= 2 && path[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
