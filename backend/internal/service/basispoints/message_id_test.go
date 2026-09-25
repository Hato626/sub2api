package basispoints

import (
	"strings"
	"testing"
)

func TestPrepareNormalizesLegacyItemMessageIDs(t *testing.T) {
	source := testSource()
	source["input"] = []any{object{"type": "message", "id": "item_502ce2d780ee31d73e097298", "role": "user", "content": []any{object{"type": "input_text", "text": "continue"}}}}
	first, _ := mustPrepare(t, source, "account/key", nil)
	second, _ := mustPrepare(t, source, "account/key", nil)
	items := first["input"].([]any)
	var id string
	for _, raw := range items {
		item := raw.(object)
		if item["type"] == "message" && text(item["id"]) != "" {
			id = text(item["id"])
			break
		}
	}
	if !strings.HasPrefix(id, "msg_") {
		t.Fatalf("message ID was not normalized: %q", id)
	}
	var secondID string
	for _, raw := range second["input"].([]any) {
		item := raw.(object)
		if item["type"] == "message" && text(item["id"]) != "" {
			secondID = text(item["id"])
			break
		}
	}
	if id != secondID {
		t.Fatalf("message ID is not stable")
	}
}
