package main

import (
	"strings"
	"testing"
)

func TestStripContextRoundTrip(t *testing.T) {
	for _, context := range []string{"WoW Forever 1.60.1.70009 · interface 16001\nCharacter: level 12", " \n"} {
		sent := withContext("Where next?\nThanks", context, 1790000000)
		if !strings.Contains(sent, `captured="2026-09-21T14:13:20Z"`) || !strings.Contains(sent, strings.TrimSpace(context)) {
			t.Fatalf("missing character state or capture time: %q", sent)
		}
		text, had := stripContext(sent)
		if !had || text != "Where next?\nThanks" {
			t.Fatalf("got %q %v", text, had)
		}
	}
	if sent := prompt(packet{Text: "Where next?", ContextText: "Character: level 12", Plain: true}); sent != "Where next?" {
		t.Fatalf("plain packet carried more than its text: %q", sent)
	}
	if text, had := stripContext("plain message"); had || text != "plain message" {
		t.Fatalf("plain: %q %v", text, had)
	}
}
