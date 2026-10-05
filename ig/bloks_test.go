package ig

import (
	"encoding/json"
	"testing"
)

func TestBloksAACSkipsEmptyPlaceholder(t *testing.T) {
	var res map[string]any
	raw := `{"layout":{"bloks_payload":{"data":[
		{"data":{"initial_lispy":"\t(fhy \"\")","key":"CAA_ACCOUNT_ACCESS_CONTEXT:aac","mode":"d"},"type":"gs"},
		{"data":{"initial":false},"type":"ls"},
		{"data":{"initial_lispy":"\t(fhy \"TOKEN-123\")","key":"CAA_ACCOUNT_ACCESS_CONTEXT:aac","mode":"p"},"type":"gs"}
	]}}}`
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if got := bloksAAC(res); got != "TOKEN-123" {
		t.Fatalf("aac = %q", got)
	}
}
