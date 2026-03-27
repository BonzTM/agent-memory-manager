package service

import "testing"

func TestSanitizeTightDescription(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantOK  bool
		wantOut string
	}{
		{"empty string", "", false, ""},
		{"whitespace only", "   ", false, ""},
		{"json object", `{"tool_name":"Read","tool_input":{}}`, false, `{"tool_name":"Read","tool_input":{}}`},
		{"json array", `["a","b"]`, false, `["a","b"]`},
		{"contains tool_name key", `summary with "tool_name" inside`, false, `summary with "tool_name" inside`},
		{"contains tool_input key", `result "tool_input": {...}`, false, `result "tool_input": {...}`},
		{"tool_name colon variant", "tool_name: Read", false, "tool_name: Read"},
		{"tool_input colon variant", "tool_input: {}", false, "tool_input: {}"},
		{"mixed case marker", `TOOL_NAME: Read`, false, `TOOL_NAME: Read`},
		{"exceeds maxTightDescLen", string(make([]byte, maxTightDescLen+1)), false, string(make([]byte, maxTightDescLen+1))},
		{"exactly maxTightDescLen", string(make([]byte, maxTightDescLen)), true, string(make([]byte, maxTightDescLen))},
		{"clean short phrase", "Agent updated user preferences", true, "Agent updated user preferences"},
		{"clean phrase with leading whitespace", "  Agent recalled memory  ", true, "Agent recalled memory"},
		{"brace not at start is fine", "Decision: use {sqlite} backend", true, "Decision: use {sqlite} backend"},
		{"bracket not at start is fine", "Options [a, b] were considered", true, "Options [a, b] were considered"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, ok := sanitizeTightDescription(tc.input)
			if ok != tc.wantOK {
				t.Errorf("ok: got %v, want %v (input: %q)", ok, tc.wantOK, tc.input)
			}
			if ok && out != tc.wantOut {
				t.Errorf("out: got %q, want %q", out, tc.wantOut)
			}
		})
	}
}

func TestSanitizeSnippet(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
		{"json object", `{"tool_name":"Read"}`, ""},
		{"json array", `["a"]`, ""},
		{"contains tool_name key", `x "tool_name" y`, ""},
		{"contains tool_input key", `x "tool_input" y`, ""},
		{"clean content", "User set preference to dark mode", "User set preference to dark mode"},
		{"leading whitespace trimmed", "  hello  ", "hello"},
		{"brace mid-string is fine", "Use {sqlite}", "Use {sqlite}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeSnippet(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q (input: %q)", got, tc.want, tc.input)
			}
		})
	}
}
