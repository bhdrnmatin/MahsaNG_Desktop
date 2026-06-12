package core

import "testing"

// TestBuildInstanceFragment checks that the fragment outbound and the injected
// streamSettings.sockopt.dialerProxy form a config the xray loader accepts —
// the one part of the fragmentation change the JSON shape could get wrong.
func TestBuildInstanceFragment(t *testing.T) {
	ob := []byte(`{"protocol":"trojan","tag":"proxy",` +
		`"settings":{"servers":[{"address":"example.com","port":443,"password":"x"}]},` +
		`"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"example.com"}}}`)

	inst, err := buildInstance(ob, true, nil)
	if err != nil {
		t.Fatalf("buildInstance with fragment failed to load: %v", err)
	}
	inst.Close()
}
