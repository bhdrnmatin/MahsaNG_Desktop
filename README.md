# MahsaNG Desktop

A cross-platform desktop VPN client in Go — an independent desktop port of the
MahsaNG Android app. It fetches free
v2ray/xray configs from public sources, latency-tests them concurrently, and
routes your whole system through the server you pick.

Built with [Fyne](https://fyne.io) for the UI and
[xray-core](https://github.com/XTLS/Xray-core) embedded as a library (no
external binaries), with system-wide routing via
[tun2socks](https://github.com/xjasonlyu/tun2socks).

## Features

- **GET CONFIG** — pulls fresh configs from several public aggregators at once,
  de-duplicates, and keeps a weighted spread across sources
- **TEST ALL** — measures real latency (HTTPS probe through each server) with
  40 concurrent workers; **SORT** orders fastest-first
- **Speed Test** — download throughput check for a single server
- **Connect** — one click starts a local SOCKS5/HTTP proxy *and* a TUN device
  that routes all OS traffic through the server (apps that ignore proxy
  settings included)
- **Clipboard import** — paste a share link, a list, or a whole base64
  subscription blob
- Supported links: `vmess://`, `vless://`, `trojan://`, `ss://`
  (transports: tcp, ws, grpc, http/h2 — security: none, tls, reality)

## Download

Grab the latest binary for your OS from the
[Releases page](../../releases) — no install, no dependencies:

| OS | Asset | Notes |
|---|---|---|
| Linux | `mahsang-linux-amd64.tar.gz` | run the binary |
| Windows | `mahsang-windows-amd64.zip` | keep `wintun.dll` next to the exe |
| macOS | `mahsang-macos-arm64.tar.gz` | Apple Silicon; first launch: right-click → Open (unsigned) |

Everything works unprivileged **except Connect**, which creates a TUN device
and edits the routing table:

- Linux / macOS: `sudo ./mahsang`
- Windows: right-click → *Run as administrator*

## Usage

1. **GET CONFIG** (or paste your own subscription/links via the **+** button)
2. **TEST ALL**, then **SORT** — dead servers show a red `-1ms`
3. *(optional)* **Delete Invalid** to prune dead servers
4. Select a server and press **Connect** — the whole system now routes
   through it. Press **Disconnect** to restore normal routing.

## Build from source

Requires Go (see `go.mod` for the version) and a C compiler (Fyne uses cgo).

```bash
# Linux build deps for Fyne
sudo apt install gcc libgl1-mesa-dev xorg-dev

go build ./cmd/mahsang     # GUI
go build ./cmd/mahsang-cli # headless CLI
```

On Windows use a MinGW-w64 gcc (e.g. MSYS2); on macOS install the Xcode
command-line tools.

### CLI

`mahsang-cli` exercises the same engine without a GUI:

```
mahsang-cli get               # fetch from built-in providers, list configs
mahsang-cli test <subURL|->   # fetch a subscription URL, ping all, sorted
mahsang-cli ping <share-link> # measure one link
mahsang-cli connect <link>    # local SOCKS tunnel + egress IP check
mahsang-cli speed <link>      # download throughput through one link
```

## How it works

```
provider  →  parser  →  tester  →  core (xray-core)  →  tun (tun2socks)
fetch links  link →      40-worker   SOCKS/HTTP proxy +   TUN device +
from sources  outbound    ping pool   latency/speed probe   route override
              JSON
```

- Config sources implement a small `Provider` interface, so adding a new feed
  is one constructor call (see `internal/provider/builtin.go`)
- Connect excludes the VPN server's own IP from the TUN routes to avoid a
  routing loop, and restores the routing table on disconnect/exit

> **Note on the original Mahsa feed:** mahsaserver.com delivers configs over a
> closed, authenticated protocol that is deliberately unpublished, so this
> port ships public aggregators instead. If that protocol ever becomes
> legitimately available, it slots in as just another `Provider`.

## Known limitations

- Connect requires the whole app to run elevated (privilege separation is
  planned)
- DNS requests to a LAN resolver (e.g. your router) bypass the tunnel —
  use a public DNS while connected
- Windows and macOS TUN routing is new and less battle-tested than Linux —
  issue reports welcome
- Free public configs come and go; expect to re-run GET CONFIG / TEST ALL

## Credits

Powered by **MATOK** — Telegram [@mat_bh](https://t.me/mat_bh)
