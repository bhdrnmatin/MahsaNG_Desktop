package provider

// Built-in providers shipped with the app. Users can add their own
// Subscription entries on top of these.
//
// On the "Mahsa servers" source: MahsaNG's primary rotating feed from
// mahsaserver.com is delivered over a closed, authenticated, encrypted protocol
// that is deliberately not published (see the project README). We therefore
// cannot reproduce that exact endpoint, and the public collector the app used
// to reference (YeBeKhe) has since been taken down. Instead we ship several
// live public free-config aggregators. If the authenticated mahsaserver
// protocol is later obtained legitimately (e.g. from the maintainers, who run
// the open NikaNG fork), it slots in as just another Provider implementation
// without touching the rest of the app.
//
// NOTE: these are community-run sources whose availability changes over time.
// Multiple are registered so one going offline does not break "GET CONFIG".
var builtinSources = []struct{ name, url string }{
	{"Epodonios", "https://raw.githubusercontent.com/Epodonios/v2ray-configs/main/All_Configs_Sub.txt"},
	{"Aggregator", "https://raw.githubusercontent.com/mahdibland/V2RayAggregator/master/sub/sub_merge.txt"},
	{"BarryFar", "https://raw.githubusercontent.com/barry-far/V2ray-Config/main/All_Configs_Sub.txt"},
	{"MhdiTaheri", "https://raw.githubusercontent.com/MhdiTaheri/V2rayCollector/main/sub/mix"},
	{"RU-White-All", "https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/refs/heads/main/WHITE-CIDR-RU-all.txt"},
	{"RU-White-Checked", "https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/refs/heads/main/WHITE-CIDR-RU-checked.txt"},
	{"RU-White-SNI", "https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/refs/heads/main/WHITE-SNI-RU-all.txt"},
	{"BarryFar-VLESS", "https://raw.githubusercontent.com/barry-far/V2ray-config/main/Splitted-By-Protocol/vless.txt"},
}

// Builtins returns the providers enabled by default.
func Builtins() []Provider {
	out := make([]Provider, 0, len(builtinSources))
	for _, s := range builtinSources {
		out = append(out, NewSubscription(s.name, s.url))
	}
	return out
}
