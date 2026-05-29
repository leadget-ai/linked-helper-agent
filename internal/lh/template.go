// Package lh — message template parser/renderer for Linked Helper's
// action_configs.actionSettings.messageTemplate payload.
//
// LH stores messages as a tree:
//
//	{ "type": "variants",
//	  "variants": [
//	    { "type": "variant",
//	      "child": { "type": "group",
//	                 "children": [
//	                   { "type": "var",  "name": "firstName" },
//	                   { "type": "text", "value": ", let's chat..." }
//	                 ] } } ] }
//
// We walk the tree twice per variant:
//   - keepPlaceholders=true  → "{firstName}, let's chat..." (template)
//   - keepPlaceholders=false → "John, let's chat..."        (rendered example)
//
// V1: we only emit the FIRST variant. A/B/C splitting is a planned follow-up
// once the platform CampaignMessage write path keys on (seq, variantLabel).
package lh

import (
	"encoding/json"
	"strings"
)

// RenderMessage parses an actionSettings.messageTemplate blob and returns
// (template, example) strings for the first variant. Returns ("", "", false)
// when the tree is malformed or has no variants — caller treats this as a
// non-messaging or empty step.
func RenderMessage(actionSettingsJSON string) (template, example string, ok bool) {
	if actionSettingsJSON == "" {
		return "", "", false
	}
	var root struct {
		MessageTemplate *node `json:"messageTemplate"`
	}
	if err := json.Unmarshal([]byte(actionSettingsJSON), &root); err != nil {
		return "", "", false
	}
	if root.MessageTemplate == nil {
		return "", "", false
	}

	variant := firstVariant(root.MessageTemplate)
	if variant == nil {
		return "", "", false
	}

	template = renderNode(variant, true)
	example = renderNode(variant, false)
	// Empty bodies are common for InvitePerson configs (no note set) — treat
	// as "no message" to keep the wire payload tidy.
	if strings.TrimSpace(template) == "" && strings.TrimSpace(example) == "" {
		return "", "", false
	}
	return template, example, true
}

// node mirrors the recursive shape of the LH template tree. All fields are
// optional and only populated for their matching `type`.
type node struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Name     string `json:"name"`
	Variants []node `json:"variants"`
	Child    *node  `json:"child"`
	Children []node `json:"children"`
}

// firstVariant returns the child of the first non-empty variant. Skips empty
// variants ({"children":[]}) so InvitePerson configs with a blank first
// variant fall through to the next one before we declare the template empty.
func firstVariant(root *node) *node {
	if root == nil || root.Type != "variants" {
		return nil
	}
	for i := range root.Variants {
		v := &root.Variants[i]
		if v.Type != "variant" || v.Child == nil {
			continue
		}
		if hasContent(v.Child) {
			return v.Child
		}
	}
	// All variants are empty — return the first one anyway so caller's
	// emptiness check kicks in and we report "no message" cleanly.
	if len(root.Variants) > 0 {
		return root.Variants[0].Child
	}
	return nil
}

func hasContent(n *node) bool {
	if n == nil {
		return false
	}
	switch n.Type {
	case "text":
		return strings.TrimSpace(n.Value) != ""
	case "var":
		return n.Name != ""
	case "group":
		for i := range n.Children {
			if hasContent(&n.Children[i]) {
				return true
			}
		}
	}
	return false
}

// renderNode walks the tree depth-first concatenating text. `keepPlaceholders`
// switches between template ({firstName}) and example (John) output for `var`
// nodes.
func renderNode(n *node, keepPlaceholders bool) string {
	if n == nil {
		return ""
	}
	switch n.Type {
	case "text":
		return n.Value
	case "var":
		if keepPlaceholders {
			return "{" + n.Name + "}"
		}
		return sampleFor(n.Name)
	case "group":
		var b strings.Builder
		for i := range n.Children {
			b.WriteString(renderNode(&n.Children[i], keepPlaceholders))
		}
		return b.String()
	case "variant":
		return renderNode(n.Child, keepPlaceholders)
	}
	return ""
}

// sampleFor returns a believable sample value for a LH variable name. The map
// covers the variables LH exposes by default; unknown names fall back to a
// brace-wrapped placeholder so the example still tells the reader something
// is going to be substituted there at send time.
func sampleFor(name string) string {
	switch name {
	case "firstName":
		return "John"
	case "lastName":
		return "Doe"
	case "fullName", "name":
		return "John Doe"
	case "companyName", "company":
		return "Acme Inc"
	case "position", "title", "jobTitle":
		return "Head of Sales"
	case "industry":
		return "Software"
	case "city", "location":
		return "San Francisco"
	case "country":
		return "United States"
	case "email":
		return "john.doe@example.com"
	case "myFirstName":
		return "Pavel"
	case "myFullName":
		return "Pavel Luhin"
	case "myCompanyName":
		return "Leadget"
	default:
		return "{" + name + "}"
	}
}
