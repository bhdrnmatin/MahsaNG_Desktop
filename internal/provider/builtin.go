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
//
// weight is how many configs a provider contributes per round in GET CONFIG's
// selection: higher = a larger share of the capped list. Weights favour the
// Iran-oriented aggregators; the RU-White-* sources are whitelist configs
// built for users inside Russia (VK-SNI tricks) and near-never work from
// other countries, so they get the minimum share rather than the largest
// (field-tested 2026-06: every sampled RU-White config failed from Iran).
var builtinSources = []struct {
	name, url string
	weight    int
}{
	{"Epodonios", "https://raw.githubusercontent.com/Epodonios/v2ray-configs/main/All_Configs_Sub.txt", 3},
	{"BarryFar-VLESS", "https://raw.githubusercontent.com/barry-far/V2ray-config/main/Splitted-By-Protocol/vless.txt", 3},
	{"BarryFar", "https://raw.githubusercontent.com/barry-far/V2ray-Config/main/All_Configs_Sub.txt", 2},
	{"Aggregator", "https://raw.githubusercontent.com/mahdibland/V2RayAggregator/master/sub/sub_merge.txt", 2},
	{"MhdiTaheri", "https://raw.githubusercontent.com/MhdiTaheri/V2rayCollector/main/sub/mix", 2},
	{"RU-White-Checked", "https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/refs/heads/main/WHITE-CIDR-RU-checked.txt", 1},
	{"RU-White-All", "https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/refs/heads/main/WHITE-CIDR-RU-all.txt", 1},
	{"RU-White-SNI", "https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/refs/heads/main/WHITE-SNI-RU-all.txt", 1},
}

// Builtins returns the providers enabled by default.
func Builtins() []Provider {
	out := make([]Provider, 0, len(builtinSources))
	for _, s := range builtinSources {
		out = append(out, NewWeightedSubscription(s.name, s.url, s.weight))
	}
	return out
}
