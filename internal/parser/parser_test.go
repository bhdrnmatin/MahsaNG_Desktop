package parser

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// outbound unmarshals the parts of the generated xray outbound JSON the tests
// assert on.
type outbound struct {
	Protocol string `json:"protocol"`
	Settings struct {
		Vnext []struct {
			Address string `json:"address"`
			Port    int    `json:"port"`
		} `json:"vnext"`
		Servers []struct {
			Address  string `json:"address"`
			Port     int    `json:"port"`
			Method   string `json:"method"`
			Password string `json:"password"`
		} `json:"servers"`
	} `json:"settings"`
	StreamSettings struct {
		Network  string `json:"network"`
		Security string `json:"security"`
	} `json:"streamSettings"`
}

func mustOutbound(t *testing.T, link string) (string, outbound) {
	t.Helper()
	c, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse(%q): %v", link, err)
	}
	var ob outbound
	if err := json.Unmarshal(c.Outbound, &ob); err != nil {
		t.Fatalf("outbound not valid JSON: %v", err)
	}
	return c.Name, ob
}

func TestParseVMess(t *testing.T) {
	js := `{"ps":"my server","add":"1.2.3.4","port":"443","id":"uuid-1","aid":0,"net":"ws","tls":"tls","host":"h.example"}`
	link := "vmess://" + base64.StdEncoding.EncodeToString([]byte(js))

	name, ob := mustOutbound(t, link)
	if name != "my server" || ob.Protocol != "vmess" {
		t.Fatalf("got name=%q protocol=%q", name, ob.Protocol)
	}
	if v := ob.Settings.Vnext[0]; v.Address != "1.2.3.4" || v.Port != 443 {
		t.Fatalf("vnext = %+v", v)
	}
	if ob.StreamSettings.Network != "ws" || ob.StreamSettings.Security != "tls" {
		t.Fatalf("stream = %+v", ob.StreamSettings)
	}
}

func TestParseVLESSReality(t *testing.T) {
	link := "vless://uuid-2@example.com:8443?type=grpc&security=reality&pbk=KEY&sni=cdn.example&fp=chrome#Fast%20One"

	name, ob := mustOutbound(t, link)
	if name != "Fast One" || ob.Protocol != "vless" {
		t.Fatalf("got name=%q protocol=%q", name, ob.Protocol)
	}
	if v := ob.Settings.Vnext[0]; v.Address != "example.com" || v.Port != 8443 {
		t.Fatalf("vnext = %+v", v)
	}
	if ob.StreamSettings.Security != "reality" {
		t.Fatalf("security = %q, want reality", ob.StreamSettings.Security)
	}
}

func TestParseTrojan(t *testing.T) {
	_, ob := mustOutbound(t, "trojan://secret@host.example:443?security=tls#tj")
	if ob.Protocol != "trojan" {
		t.Fatalf("protocol = %q", ob.Protocol)
	}
	if s := ob.Settings.Servers[0]; s.Address != "host.example" || s.Port != 443 || s.Password != "secret" {
		t.Fatalf("server = %+v", s)
	}
}

func TestParseShadowsocks(t *testing.T) {
	userInfo := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pw"))
	cases := []struct {
		name, link, wantHost string
		wantPort             int
	}{
		{"sip002", "ss://" + userInfo + "@9.9.9.9:8388#srv", "9.9.9.9", 8388},
		{"sip002-ipv6", "ss://" + userInfo + "@[2001:db8::1]:8388#v6", "2001:db8::1", 8388},
		{"legacy", "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pw@9.9.9.9:8388")), "9.9.9.9", 8388},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ob := mustOutbound(t, tc.link)
			s := ob.Settings.Servers[0]
			if s.Address != tc.wantHost || s.Port != tc.wantPort {
				t.Fatalf("server = %+v, want %s:%d", s, tc.wantHost, tc.wantPort)
			}
			if s.Method != "aes-256-gcm" || s.Password != "pw" {
				t.Fatalf("creds = %+v", s)
			}
		})
	}
}

func TestParseUnsupported(t *testing.T) {
	if _, err := Parse("wireguard://whatever"); err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}

// TestParseRejectsXHTTP: xhttp/splithttp configs must be dropped at parse so the
// xray-core splithttp dialer's nil-deref panic can never be triggered.
func TestParseRejectsXHTTP(t *testing.T) {
	links := []string{
		"vless://uuid@example.com:443?type=xhttp&security=tls&sni=a.com&path=%2Fx#x",
		"vless://uuid@example.com:443?type=splithttp&security=reality&pbk=K&sni=a.com#x",
		"trojan://pw@example.com:443?type=xhttp&security=tls#x",
	}
	for _, l := range links {
		if _, err := Parse(l); err == nil {
			t.Errorf("expected xhttp/splithttp to be rejected: %s", l)
		}
	}
	// A normal ws config must still parse fine (guard isn't too broad).
	if _, err := Parse("vless://uuid@example.com:443?type=ws&security=tls&host=a.com&path=%2Fx#x"); err != nil {
		t.Errorf("ws config wrongly rejected: %v", err)
	}
}
