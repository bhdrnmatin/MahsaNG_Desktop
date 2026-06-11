package core

import (
	"encoding/json"
	"fmt"
)

// OutboundServer returns the remote server address (host or IP) of an outbound,
// used by TUN routing to exclude the real server from the tunnel (avoiding a
// routing loop). Works for vmess/vless (settings.vnext) and trojan/shadowsocks
// (settings.servers).
func OutboundServer(outboundJSON []byte) (string, error) {
	var ob struct {
		Settings struct {
			Vnext []struct {
				Address string `json:"address"`
			} `json:"vnext"`
			Servers []struct {
				Address string `json:"address"`
			} `json:"servers"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(outboundJSON, &ob); err != nil {
		return "", err
	}
	if len(ob.Settings.Vnext) > 0 && ob.Settings.Vnext[0].Address != "" {
		return ob.Settings.Vnext[0].Address, nil
	}
	if len(ob.Settings.Servers) > 0 && ob.Settings.Servers[0].Address != "" {
		return ob.Settings.Servers[0].Address, nil
	}
	return "", fmt.Errorf("no server address in outbound")
}
