package config

import "testing"

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

func TestNormalizeAgentName(t *testing.T) {
	got, ok := NormalizeAgentName("  edda  ")
	if !ok || got != "edda" {
		t.Fatalf("NormalizeAgentName trimmed = (%q, %v), want (edda, true)", got, ok)
	}
	for _, value := range []string{"", "two words", "bad:name", "bad>name", "line\nbreak"} {
		if got, ok := NormalizeAgentName(value); ok {
			t.Fatalf("NormalizeAgentName(%q) = (%q, true), want false", value, got)
		}
	}
}

func TestEffectiveAgentNamePrecedence(t *testing.T) {
	t.Setenv("AIDUR_AGENT_NAME", "envy")
	got, src := EffectiveAgentName(Config{AgentName: "cfg"})
	if got != "envy" || src != "AIDUR_AGENT_NAME" {
		t.Fatalf("EffectiveAgentName env = (%q, %q), want (envy, AIDUR_AGENT_NAME)", got, src)
	}
}

func TestEffectiveAgentNameConfigAndDefault(t *testing.T) {
	t.Setenv("AIDUR_AGENT_NAME", "")
	got, src := EffectiveAgentName(Config{AgentName: "cfg"})
	if got != "cfg" || src != "config" {
		t.Fatalf("EffectiveAgentName config = (%q, %q), want (cfg, config)", got, src)
	}
	got, src = EffectiveAgentName(Config{})
	if got != DefaultAgentName || src != "default" {
		t.Fatalf("EffectiveAgentName default = (%q, %q), want (%q, default)", got, src, DefaultAgentName)
	}
}
