package provider

import (
	"strings"
	"testing"
)

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
