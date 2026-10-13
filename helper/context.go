package main

import (
	"fmt"
	"strings"
	"time"
)

const (
	contextOpen  = "<wow_character_state"
	contextClose = "</wow_character_state>"
	linkNote     = "Your reply is shown inside World of Warcraft: write an item or spell as [Name](item:ID) or [Name](spell:ID) when you have its ID."
)

// Plain means the player turned off all extras. Then the AI gets only the player's text.
func prompt(p packet) string {
	if p.Plain {
		return p.Text
	}
	return withContext(p.Text, p.ContextText, int64(p.At))
}

func withContext(text, contextText string, at int64) string {
	when := time.Now()
	if at > 0 {
		when = time.Unix(at, 0)
	}
	block := linkNote
	if state := strings.TrimSpace(contextText); state != "" {
		block += "\n" + state
	}
	return fmt.Sprintf("%s captured=\"%s\" source=\"thott.bot chat addon\">\n%s\n%s\n\n%s",
		contextOpen, when.UTC().Format(time.RFC3339), block, contextClose, text)
}

func stripContext(text string) (string, bool) {
	t := strings.TrimLeft(text, " \t\r\n")
	if !strings.HasPrefix(t, contextOpen) {
		return text, false
	}
	end := strings.Index(t, contextClose)
	if end < 0 {
		return text, false
	}
	return strings.TrimLeft(t[end+len(contextClose):], " \t\r\n"), true
}
