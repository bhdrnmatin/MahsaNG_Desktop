package provider

import (
	"encoding/base64"
	"strings"
)

// tryBase64Block reports whether the whole body is a single base64 blob (as many
// subscription endpoints return) and, if so, returns the decoded text.
func tryBase64Block(text string) (string, bool) {
	compact := strings.Join(strings.Fields(text), "")
	if len(compact) < 8 {
		return "", false
	}
	s := strings.ReplaceAll(compact, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	dec, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", false
	}
	out := string(dec)
	// Only treat as base64 if it actually decoded to share links.
	if strings.Contains(out, "://") {
		return out, true
	}
	return "", false
}
