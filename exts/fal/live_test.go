package fal

import (
	"encoding/json"
	"os"
	"testing"
)

// TestLiveIllustrate talks to the real fal. It is skipped unless COGDEBT_LIVE
// is set, because generating an image costs money — nobody should pay for it by
// running `go test ./...`.
//
//	COGDEBT_LIVE=1 go test ./exts/fal/ -run Live -v
func TestLiveIllustrate(t *testing.T) {
	if os.Getenv("COGDEBT_LIVE") == "" || os.Getenv(EnvKey) == "" {
		t.Skip("set COGDEBT_LIVE=1 and " + EnvKey + " to run against the real service")
	}

	e := New()
	args, _ := json.Marshal(map[string]any{
		"source":      "a thermostat",
		"target":      "a TCP congestion window",
		"shared_role": "negative_feedback_controller",
		"breakdown":   "a thermostat senses the room directly; TCP infers loss after the fact",
	})

	raw, err := e.Invoke(t.Context(), "illustrate", args)
	if err != nil {
		t.Fatalf("illustrate against the real service: %v", err)
	}

	var got struct {
		URL    string `json:"url"`
		Source string `json:"source"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.URL == "" {
		t.Fatal("no image url; the response shape assumed in Invoke does not match the real one")
	}
	t.Logf("image: %s", got.URL)
}
