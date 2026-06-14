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
// Iran-oriented sources. Dropped 2026-06: the RU-White whitelist sources (built
// for Russia; field-tested, every sampled one failed from Iran) and the
// mahdibland Aggregator (its feed went empty).
var builtinSources = []struct {
	name, url string
	weight    int
}{
	// Iran-oriented, auto-tested "healthy only", carries REALITY.
	{"Freedom-VLESS", "https://raw.githubusercontent.com/MahanKenway/Freedom-V2Ray/main/configs/vless.txt", 3},
	{"Epodonios", "https://raw.githubusercontent.com/Epodonios/v2ray-configs/main/All_Configs_Sub.txt", 3},
	{"BarryFar-VLESS", "https://raw.githubusercontent.com/barry-far/V2ray-config/main/Splitted-By-Protocol/vless.txt", 3},
	// Filtered subscription, carries REALITY.
	{"MatinGhanbari-VLESS", "https://raw.githubusercontent.com/MatinGhanbari/v2ray-configs/main/subscriptions/filtered/subs/vless.txt", 2},
	{"BarryFar", "https://raw.githubusercontent.com/barry-far/V2ray-Config/main/All_Configs_Sub.txt", 2},
	{"MhdiTaheri", "https://raw.githubusercontent.com/MhdiTaheri/V2rayCollector/main/sub/mix", 2},
}

// serverlessURL is the patterniha/Serverless-for-Iran feed: whole xray configs
// (fragment/noise/routing, no proxy server) that bypass filtering server-side.
// Unlike the scraped sources these never go dead, so they ride along as a
// reliable fallback that's always present in the list.
const serverlessURL = "https://raw.githubusercontent.com/patterniha/Serverless-for-Iran/refs/heads/main/Subscription/Serverless-for-Iran.json"

// Builtins returns the providers enabled by default.
func Builtins() []Provider {
	out := make([]Provider, 0, len(builtinSources)+1)
	for _, s := range builtinSources {
		out = append(out, NewWeightedSubscription(s.name, s.url, s.weight))
	}
	out = append(out, NewServerless("Serverless", serverlessURL))
	return out
}
