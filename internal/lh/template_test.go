package lh

import "testing"

func TestRenderMessage_FirstVariantWithVarsAndText(t *testing.T) {
	// Shape lifted from a real action_configs.actionSettings blob.
	in := `{
		"messageTemplate": {
			"type": "variants",
			"variants": [
				{ "type": "variant",
				  "child": { "type": "group",
				             "children": [
				               {"type":"var","name":"firstName"},
				               {"type":"text","value":", could we schedule a virtual coffee?"}
				             ]}}
			]
		}
	}`
	tpl, ex, ok := RenderMessage(in)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "{firstName}, could we schedule a virtual coffee?"; tpl != want {
		t.Errorf("template = %q, want %q", tpl, want)
	}
	if want := "John, could we schedule a virtual coffee?"; ex != want {
		t.Errorf("example = %q, want %q", ex, want)
	}
}

func TestRenderMessage_SkipsEmptyFirstVariant(t *testing.T) {
	// InvitePerson configs often ship with the first variant blank — we
	// should fall through to the next one with content.
	in := `{
		"messageTemplate": {
			"type": "variants",
			"variants": [
				{ "type": "variant", "child": { "type": "group", "children": [] } },
				{ "type": "variant",
				  "child": { "type": "group",
				             "children": [
				               {"type":"text","value":"Hi "},
				               {"type":"var","name":"firstName"}
				             ]}}
			]
		}
	}`
	tpl, ex, ok := RenderMessage(in)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tpl != "Hi {firstName}" || ex != "Hi John" {
		t.Errorf("got tpl=%q ex=%q", tpl, ex)
	}
}

func TestRenderMessage_AllEmpty(t *testing.T) {
	in := `{"messageTemplate":{"type":"variants","variants":[{"type":"variant","child":{"type":"group","children":[]}}]}}`
	if _, _, ok := RenderMessage(in); ok {
		t.Error("expected ok=false for empty template")
	}
}

func TestRenderMessage_Malformed(t *testing.T) {
	if _, _, ok := RenderMessage("{not json"); ok {
		t.Error("expected ok=false on parse error")
	}
}

func TestRenderMessage_UnknownVarKeepsPlaceholder(t *testing.T) {
	in := `{"messageTemplate":{"type":"variants","variants":[{"type":"variant","child":{"type":"group","children":[
		{"type":"var","name":"customField42"},
		{"type":"text","value":" rocks"}
	]}}]}}`
	tpl, ex, ok := RenderMessage(in)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tpl != "{customField42} rocks" || ex != "{customField42} rocks" {
		t.Errorf("got tpl=%q ex=%q", tpl, ex)
	}
}
