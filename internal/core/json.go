package core

import "encoding/json"

// mustObject decodes a JSON object into a generic map so it can be re-embedded
// inside a larger config. Returns nil on malformed input; buildInstance will
// then fail at the loader, which surfaces a clear error.
func mustObject(b []byte) any {
	var v any
	_ = json.Unmarshal(b, &v)
	return v
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
