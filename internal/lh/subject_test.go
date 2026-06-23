package lh

import "testing"

func TestRenderSubject(t *testing.T) {
	// The subject tree is rooted directly at a group node (no variants wrapper),
	// as InMail steps store it.
	valid := `{"subjectTemplate":{"type":"group","children":[{"type":"text","value":"Hi "},{"type":"var","name":"firstName"}]}}`
	tpl, ex, ok := RenderSubject(valid)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tpl != "Hi {firstName}" {
		t.Errorf("template = %q, want %q", tpl, "Hi {firstName}")
	}
	if ex != "Hi John" {
		t.Errorf("example = %q, want %q", ex, "Hi John")
	}
}

func TestRenderSubject_AbsentOrEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"no subjectTemplate key", `{"messageTemplate":{"type":"variants","variants":[]}}`},
		{"blank subject", `{"subjectTemplate":{"type":"group","children":[{"type":"text","value":"   "}]}}`},
		{"empty string", ``},
		{"malformed json", `{"subjectTemplate": not json}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := RenderSubject(tc.in); ok {
				t.Errorf("RenderSubject(%q) ok = true, want false", tc.in)
			}
		})
	}
}

func TestParseWaitMs(t *testing.T) {
	if ms, ok := parseWaitMs(`{"moveToSuccessfulAfterMs":345600000}`); !ok || ms != 345600000 {
		t.Errorf("parseWaitMs = (%d,%v), want (345600000,true)", ms, ok)
	}
	for _, in := range []string{``, `{}`, `{"moveToSuccessfulAfterMs":0}`, `{"moveToSuccessfulAfterMs":-1}`, `{bad`} {
		if _, ok := parseWaitMs(in); ok {
			t.Errorf("parseWaitMs(%q) ok = true, want false", in)
		}
	}
}
