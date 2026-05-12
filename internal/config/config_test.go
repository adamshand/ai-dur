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
