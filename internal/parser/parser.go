// Package parser converts VPN share links into xray-core outbound JSON.
//
// Supported schemes: vmess://, vless://, trojan://, ss:// (Shadowsocks).
// Transports: tcp, ws, grpc, http/h2; security: none, tls, reality.
// This covers the formats MahsaNG / v2rayNG subscriptions use in practice.
package parser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"mahsang/internal/model"
)

// Parse turns a single share link into a model.Config with its Outbound filled.
func Parse(link string) (model.Config, error) {
	link = strings.TrimSpace(link)
	switch {
	case strings.HasPrefix(link, "vmess://"):
		return parseVMess(link)
	case strings.HasPrefix(link, "vless://"):
		return parseVLESS(link)
	case strings.HasPrefix(link, "trojan://"):
		return parseTrojan(link)
	case strings.HasPrefix(link, "ss://"):
		return parseShadowsocks(link)
	default:
		return model.Config{}, fmt.Errorf("unsupported scheme: %.12q", link)
	}
}

// --- VMess: vmess://<base64 of a JSON object> -------------------------------

type vmessLink struct {
	Ps   string `json:"ps"`
	Add  string `json:"add"`
	Port any    `json:"port"`
	ID   string `json:"id"`
	Aid  any    `json:"aid"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host"`
	Path string `json:"path"`
	TLS  string `json:"tls"`
	SNI  string `json:"sni"`
	Alpn string `json:"alpn"`
}

func parseVMess(link string) (model.Config, error) {
	raw, err := b64decode(strings.TrimPrefix(link, "vmess://"))
	if err != nil {
		return model.Config{}, fmt.Errorf("vmess base64: %w", err)
	}
	var v vmessLink
	if err := json.Unmarshal(raw, &v); err != nil {
		return model.Config{}, fmt.Errorf("vmess json: %w", err)
	}
	port := toInt(v.Port)
	user := map[string]any{"id": v.ID, "alterId": toInt(v.Aid), "security": orDefault(v.Scy, "auto")}
	ob := map[string]any{
		"protocol": "vmess",
		"settings": map[string]any{
			"vnext": []any{map[string]any{
				"address": v.Add, "port": port,
				"users": []any{user},
			}},
		},
		"streamSettings": streamSettings(streamParams{
			network: orDefault(v.Net, "tcp"), security: v.TLS,
			host: v.Host, path: v.Path, sni: orDefault(v.SNI, v.Host),
			alpn: v.Alpn, headerType: v.Type,
		}),
	}
	return finish(link, orDefault(v.Ps, v.Add), "VMESS", ob)
}

// --- VLESS / Trojan: URI form ----------------------------------------------

func parseVLESS(link string) (model.Config, error) {
	u, err := url.Parse(link)
	if err != nil {
		return model.Config{}, err
	}
	q := u.Query()
	user := map[string]any{
		"id":         u.User.Username(),
		"encryption": orDefault(q.Get("encryption"), "none"),
		"flow":       q.Get("flow"),
	}
	ob := map[string]any{
		"protocol": "vless",
		"settings": map[string]any{
			"vnext": []any{map[string]any{
				"address": u.Hostname(), "port": toInt(u.Port()),
				"users": []any{user},
			}},
		},
		"streamSettings": streamSettingsFromQuery(q),
	}
	return finish(link, fragmentName(u), "VLESS", ob)
}

func parseTrojan(link string) (model.Config, error) {
	u, err := url.Parse(link)
	if err != nil {
		return model.Config{}, err
	}
	q := u.Query()
	ob := map[string]any{
		"protocol": "trojan",
		"settings": map[string]any{
			"servers": []any{map[string]any{
				"address": u.Hostname(), "port": toInt(u.Port()),
				"password": u.User.Username(),
			}},
		},
		"streamSettings": streamSettingsFromQuery(q),
	}
	return finish(link, fragmentName(u), "TROJAN", ob)
}

// --- Shadowsocks: ss://base64(method:pass)@host:port#name  or  fully base64 --

func parseShadowsocks(link string) (model.Config, error) {
	body := strings.TrimPrefix(link, "ss://")
	name := ""
	if i := strings.Index(body, "#"); i >= 0 {
		name, _ = url.QueryUnescape(body[i+1:])
		body = body[:i]
	}
	var method, password, host string
	var port int
	if at := strings.LastIndex(body, "@"); at >= 0 {
		// SIP002: base64(method:pass)@host:port
		userInfo, err := b64decode(body[:at])
		if err != nil {
			return model.Config{}, fmt.Errorf("ss userinfo: %w", err)
		}
		method, password = splitColon(string(userInfo))
		host, port = splitHostPort(body[at+1:])
	} else {
		// Legacy: base64(method:pass@host:port)
		dec, err := b64decode(body)
		if err != nil {
			return model.Config{}, fmt.Errorf("ss base64: %w", err)
		}
		s := string(dec)
		at := strings.LastIndex(s, "@")
		if at < 0 {
			return model.Config{}, fmt.Errorf("ss: malformed")
		}
		method, password = splitColon(s[:at])
		host, port = splitHostPort(s[at+1:])
	}
	ob := map[string]any{
		"protocol": "shadowsocks",
		"settings": map[string]any{
			"servers": []any{map[string]any{
				"address": host, "port": port,
				"method": method, "password": password,
			}},
		},
		"streamSettings": map[string]any{"network": "tcp"},
	}
	return finish(link, orDefault(name, host), "SHADOWSOCKS", ob)
}

// --- stream settings --------------------------------------------------------

type streamParams struct {
	network, security, host, path, sni, alpn, headerType string
	fp, pbk, sid                                         string
	serviceName                                          string
}

func streamSettingsFromQuery(q url.Values) map[string]any {
	return streamSettings(streamParams{
		network:     orDefault(q.Get("type"), "tcp"),
		security:    q.Get("security"),
		host:        q.Get("host"),
		path:        q.Get("path"),
		sni:         q.Get("sni"),
		alpn:        q.Get("alpn"),
		headerType:  q.Get("headerType"),
		fp:          q.Get("fp"),
		pbk:         q.Get("pbk"),
		sid:         q.Get("sid"),
		serviceName: q.Get("serviceName"),
	})
}

func streamSettings(p streamParams) map[string]any {
	ss := map[string]any{"network": orDefault(p.network, "tcp")}

	switch p.network {
	case "ws":
		ws := map[string]any{"path": orDefault(p.path, "/")}
		if p.host != "" {
			ws["headers"] = map[string]any{"Host": p.host}
		}
		ss["wsSettings"] = ws
	case "grpc":
		ss["grpcSettings"] = map[string]any{"serviceName": p.serviceName}
	case "http", "h2":
		ss["network"] = "http"
		h := map[string]any{"path": orDefault(p.path, "/")}
		if p.host != "" {
			h["host"] = []any{p.host}
		}
		ss["httpSettings"] = h
	default:
		if p.headerType == "http" {
			ss["tcpSettings"] = map[string]any{
				"header": map[string]any{
					"type":    "http",
					"request": map[string]any{"headers": map[string]any{"Host": []any{p.host}}},
				},
			}
		}
	}

	switch p.security {
	case "tls":
		ss["security"] = "tls"
		ss["tlsSettings"] = trimEmpty(map[string]any{
			"serverName": p.sni, "alpn": splitAlpn(p.alpn), "fingerprint": p.fp,
		})
	case "reality":
		ss["security"] = "reality"
		ss["realitySettings"] = trimEmpty(map[string]any{
			"serverName": p.sni, "fingerprint": p.fp, "publicKey": p.pbk, "shortId": p.sid,
		})
	}
	return ss
}

// --- helpers ----------------------------------------------------------------

func finish(link, name, proto string, ob map[string]any) (model.Config, error) {
	ob["tag"] = "proxy"
	js, err := json.Marshal(ob)
	if err != nil {
		return model.Config{}, err
	}
	return model.Config{
		Link: link, Name: strings.TrimSpace(name), Protocol: proto,
		Outbound: js, PingMs: model.PingUntested,
	}, nil
}

func fragmentName(u *url.URL) string {
	if u.Fragment != "" {
		if n, err := url.QueryUnescape(u.Fragment); err == nil {
			return n
		}
		return u.Fragment
	}
	return u.Hostname()
}

// b64decode tolerates both standard and URL-safe base64, padded or not.
func b64decode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	return base64.StdEncoding.DecodeString(s)
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func splitColon(s string) (a, b string) {
	a, b, _ = strings.Cut(s, ":")
	return a, b
}

// splitHostPort handles both "host:port" and bracketed IPv6 "[::1]:port"
// (splitting on the last colon, so colons inside the address are kept).
func splitHostPort(s string) (string, int) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return s, 0
	}
	host := strings.TrimSuffix(strings.TrimPrefix(s[:i], "["), "]")
	return host, toInt(s[i+1:])
}

func splitAlpn(s string) []any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []any
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// trimEmpty drops keys whose values are empty strings or nil slices, so the
// emitted JSON stays minimal and xray defaults apply.
func trimEmpty(m map[string]any) map[string]any {
	for k, v := range m {
		switch x := v.(type) {
		case string:
			if x == "" {
				delete(m, k)
			}
		case []any:
			if len(x) == 0 {
				delete(m, k)
			}
		case nil:
			delete(m, k)
		}
	}
	return m
}
