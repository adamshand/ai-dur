package provider

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamshand/aidur/internal/config"
)

func TestPromptWithInstructions(t *testing.T) {
	if got := PromptWithInstructions("base", "   "); got != "base" {
		t.Fatalf("blank instructions changed prompt: %q", got)
	}
	got := PromptWithInstructions("base", "prefer examples")
	want := "base\n\nAdditional user instructions:\nprefer examples"
	if got != want {
		t.Fatalf("PromptWithInstructions = %q, want %q", got, want)
	}
}

func TestNewUsesConfigAPIKeyWhenEnvMissing(t *testing.T) {
	t.Setenv("OPENCODE_ZEN_API_KEY", "")
	t.Setenv("AIDUR_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{OpenCodeZenAPIKey: "config-key"}); err != nil {
		t.Fatal(err)
	}
	if got := New().APIKey; got != "config-key" {
		t.Fatalf("New().APIKey = %q, want config-key", got)
	}
}

func TestNewPrefersEnvAPIKey(t *testing.T) {
	t.Setenv("OPENCODE_ZEN_API_KEY", "env-key")
	t.Setenv("AIDUR_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{OpenCodeZenAPIKey: "config-key"}); err != nil {
		t.Fatal(err)
	}
	if got := New().APIKey; got != "env-key" {
		t.Fatalf("New().APIKey = %q, want env-key", got)
	}
}

func TestReasoningOffDisablesReasoningSpec(t *testing.T) {
	if Reasoning("off") != nil {
		t.Fatalf("Reasoning(off) != nil")
	}
	if Reasoning("") != nil {
		t.Fatalf("Reasoning(empty) != nil")
	}
	if got := Reasoning("medium"); got == nil || got.Effort != "medium" {
		t.Fatalf("Reasoning(medium) = %+v, want medium spec", got)
	}
}

func TestParseSSETextDeltas(t *testing.T) {
	body := strings.NewReader("event: response.output_text.delta\ndata: {\"delta\":\"hel\"}\n\nevent: response.output_text.delta\ndata: {\"delta\":\"lo\"}\n\nevent: response.completed\ndata: {}\n\n")
	var streamed strings.Builder
	res, err := parseSSE(body, func(delta string) { streamed.WriteString(delta) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "hello" || streamed.String() != "hello" {
		t.Fatalf("answer=%q streamed=%q", res.Answer, streamed.String())
	}
}

func TestParseSSEFunctionCallDeltas(t *testing.T) {
	body := strings.NewReader("event: response.output_item.added\ndata: {\"item\":{\"id\":\"item_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"run_readonly_command\"}}\n\nevent: response.function_call_arguments.delta\ndata: {\"item_id\":\"item_1\",\"delta\":\"{\\\"cmd\\\":\\\"pwd\\\",\"}\n\nevent: response.function_call_arguments.delta\ndata: {\"item_id\":\"item_1\",\"delta\":\"\\\"args\\\":[]}\"}\n\nevent: response.output_item.done\ndata: {\"item\":{\"id\":\"item_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"run_readonly_command\"}}\n\n")
	res, err := parseSSE(body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool calls=%d", len(res.ToolCalls))
	}
	call := res.ToolCalls[0]
	if call.CallID != "call_1" || call.Name != "run_readonly_command" || call.Arguments != "{\"cmd\":\"pwd\",\"args\":[]}" {
		t.Fatalf("unexpected call: %+v", call)
	}
}
