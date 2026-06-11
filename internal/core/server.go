package core

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
)

// firstServer extracts the remote server host and port of an outbound. Works
// for vmess/vless (settings.vnext) and trojan/shadowsocks (settings.servers).
func firstServer(outboundJSON []byte) (string, int, error) {
	var ob struct {
		Settings struct {
			Vnext []struct {
				Address string `json:"address"`
				Port    int    `json:"port"`
			} `json:"vnext"`
			Servers []struct {
				Address string `json:"address"`
				Port    int    `json:"port"`
			} `json:"servers"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(outboundJSON, &ob); err != nil {
		return "", 0, err
	}
	if len(ob.Settings.Vnext) > 0 && ob.Settings.Vnext[0].Address != "" {
		return ob.Settings.Vnext[0].Address, ob.Settings.Vnext[0].Port, nil
	}
	if len(ob.Settings.Servers) > 0 && ob.Settings.Servers[0].Address != "" {
		return ob.Settings.Servers[0].Address, ob.Settings.Servers[0].Port, nil
	}
	return "", 0, fmt.Errorf("no server address in outbound")
}

// OutboundServer returns the remote server address (host or IP) of an outbound,
// used by TUN routing to exclude the real server from the tunnel (avoiding a
// routing loop).
func OutboundServer(outboundJSON []byte) (string, error) {
	host, _, err := firstServer(outboundJSON)
	return host, err
}

// OutboundHostPort returns the outbound's remote server as "host:port", used
// by the TCP reachability pre-filter.
func OutboundHostPort(outboundJSON []byte) (string, error) {
	host, port, err := firstServer(outboundJSON)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
