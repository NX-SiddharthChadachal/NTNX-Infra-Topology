# ntnx-topo — Nutanix Cluster Topology TUI

A terminal dashboard that polls Nutanix APIs (v4.2, with v3/v2 fallback) and
renders inventory, topology, and an optional Config pane.

```
┌─ Inventory ────┬── Topology ──────────┬─ Config ─────┐
│ ▼ lab-pc       │  pe-prod             │ name   pe-1  │
│     pe-prod    │         ╭───────╮    │ extId  …     │
│     pe-dr      │         │ CVM 1 │    │ IP     10.x  │
│ ▶ remote-pc    │         ╰───────╯    │              │
│     pe-west    │         ╭────────╮   │              │
│                │         │ Host 1 │   │              │
├────────────────┴──────────────────────┴──────────────┤
│ API: v4 │ Last refresh: 2s │ Latency: 45ms           │
│ ↑↓ select  •  click node Config  •  r  •  q          │
└──────────────────────────────────────────────────────┘
```

- **Left pane** — Prism Centrals and the Prism Elements they manage. Arrow keys
  move the highlight; selecting a row updates the Topology pane.
- **Middle pane (Topology)** — `basic` is CVM / host / physical switch (grouped
  by switch). `datacenter` is an imaginary rack per switch with that switch on
  top. PC selection in **basic** shows a PC/AZ graph, with paired PCs in bands
  under a `paired` caption and unpaired ones in a `not paired` row.
- **Config pane** — opens when you click a node; click empty canvas to close.
- Two `║ ↕` dividers resize by mouse drag: Inventory/Topology and Topology/Config.

Components are colored by health:
- **Green** — healthy
- **Yellow** — warning / degraded
- **Red** — failure
- **Gray** — unknown / unreachable

## Prerequisites

- Go 1.26+ (`brew install go` on macOS)
- For the v4.2 path: AOS 7.5+, AHV 11+, Prism Central 7.5+
- A terminal at least 80x24; network access to Prism on port 9440

New to the tool? Start with the [user manual](MANUAL.md), which covers Linux, macOS
and Windows step by step. This README is the reference.

## Build

```bash
cd ~/Documents/ntnx-topo
go build -o ntnx-topo ./cmd/app/
```

## Configuration

Configuration is loaded in this priority order (highest wins):

1. CLI flags (`--pc-ip`, `--pe-ip`, `--config`, `--verify-tls`)
2. Environment variables
3. `config.yaml` file

If no Prism Central or Prism Element is configured, a setup wizard asks how many
PCs to add and collects IP / username / password for each (session only).

### config.yaml

Entirely optional — the wizard covers everything. Note that `password:` here is
plain text and `.gitignore` does not yet cover this file, so prefer the wizard
for real credentials. Nothing you type in the app is ever written back to it.

```yaml
prism_central_ip: "10.0.0.1"
prism_element_ip: "10.0.0.2"
username: "admin"
password: "secret"
poll_interval: 5s
request_timeout: 10s
insecure: true

# Optional multi-PC list (overrides a single prism_central_ip when set)
# prism_centrals:
#   - ip: "10.0.0.1"
#     username: "admin"
#     password: "secret"
```

### Environment Variables

| Variable      | Description          |
|---------------|----------------------|
| `NTNX_PC_IP`  | Prism Central IP     |
| `NTNX_PE_IP`  | Prism Element IP     |
| `NTNX_USER`   | API username         |
| `NTNX_PASS`   | API password         |
| `NTNX_VERIFY_TLS` | `true` to verify certificates |

### CLI Flags

```bash
./ntnx-topo --pc-ip 10.0.0.1 --pe-ip 10.0.0.2 --config ./my-config.yaml
```

### TLS verification

Certificate verification is **off by default**, because Prism ships self-signed certificates and
verifying them against the system roots would fail every default run. While it is off, the footer
shows a red `TLS: unverified` and credentials travel over a connection nobody validated, so on an
untrusted network prefer:

```bash
./ntnx-topo --verify-tls
```

That only works where the Prism certificate chains to a CA your system already trusts. There is no
`--ca-cert` option yet, so a self-signed Prism certificate cannot be verified — see
[docs/SECURITY_REVIEW.md](docs/SECURITY_REVIEW.md) finding 2.

### Availability zones need their own login

Availability zones discovered through a Prism Central are **not** contacted with that PC's
credentials, because the address comes from the API and a peer could choose it. They appear in the
Inventory pane as amber `login required` rows: click one (or press enter on it) and enter
credentials in the overlay, and the next poll connects. Those credentials stay in memory and are
never written to `config.yaml`.

## Usage

```bash
./ntnx-topo
```

### Keyboard Controls

| Key        | Action                         |
|------------|--------------------------------|
| `↑` / `k`  | Previous inventory row         |
| `↓` / `j`  | Next inventory row             |
| `←` / `h`  | Collapse Prism Central         |
| `→` / `l`  | Expand Prism Central           |
| `enter`    | Log in to the selected host     |
| `1` / `b` / `2` / `d` / `tab` | basic / datacenter view |
| `+` / `-`  | Zoom Topology in / out         |
| `0`        | Reset zoom and pan             |
| `shift`+arrows | Pan the Topology canvas    |
| `L`        | Toggle commands legend         |
| `esc`      | Close login overlay / Config / legend |
| `r`        | Manual refresh                 |
| `q`        | Quit                           |

Drag either wide cyan `║ ↕ ║` bar to resize (Inventory/Topology, or Topology/Config).
On the Topology canvas: scroll wheel zooms; click a node to open Config; click
empty canvas to close Config; click-drag (more than ~2 cells) pans. The wheel
scrolls the Config pane when the pointer is over it.

## Inventory discovery

When a Prism Central is configured:

1. **v4.2 `ListClusters`** classifies PC vs PE via `clusterFunction`
   (`PRISM_CENTRAL` vs `AOS`).
2. **v3 `availability_zones/list`** plus **prism domain-manager `registrations`**
   finds paired remote PCs and their addresses. Those addresses are **not** queried
   with the configured credentials, because they came from an API response — they
   appear as `login required` until you log in. See
   [Availability zones need their own login](#availability-zones-need-their-own-login).
3. Selecting a PE loads hosts via **v4.2 `ListHostsByClusterId`**.
4. Clicking a Topology node loads Config via **v4.2 `GetClusterById` /
   `GetHostById`**, or NIC/LLDP fields for a switch.

If only Prism Element is configured, v2 is used and the left pane has one row.

Fallback order on a PC: v4 → v3. PE-only: v2.

## Architecture

```
cmd/app/main.go              Entrypoint (logging, config, one tea.Program)
internal/
  config/config.go           YAML + env + flags + multi-PC list + TLS choice
  model/
    topology.go              Node, Cluster, Inventory, EntityRef, ConfigField
    state.go                 Thread-safe state + snapshots
  api/
    client.go                Shared HTTP client
    probe.go                 AZ reachability + URL host parse
    v2/                      Prism Element v2 REST
    v3/                      PC v3 REST + availability zones
    v4/                      Nutanix Go SDK v4.2 wrapper
    prism/                   Domain-manager registrations + products
  fetcher/fetcher.go         Inventory poll, selected topology, host logins
  ui/
    styles.go                Lipgloss styles
    root.go                  Wizard gate -> dashboard (single program)
    setup.go                 Credential wizard
    tui.go                   Bubbletea model (keys, mouse, Config, overlay)
    inventory.go             Left-pane tree
    split.go                 Pane widths and the two dividers
    viewport.go              Zoom, pan, hit-rect transform
    view.go                  PE topology, header, footer
    pcgraph.go               PC/AZ pairing graph
    datacenter.go            Rack-per-switch drawing
    hits.go                  Topology click/hover rects
    config.go                Config pane KV list
    reconnect.go             Session-only login overlay
    legend.go                L-key command overlay
```

Deeper detail lives in [ARCHITECTURE.md](ARCHITECTURE.md) and
[docs/CODEMAP.md](docs/CODEMAP.md).

## Logging

API errors and debug info are written to `ntnx-topo.log` in the working
directory, keeping the terminal output clean for the TUI. `slog`, the SDK's
logrus instance and `os.Stderr` are all redirected there. There is no rotation,
so delete it occasionally.

## Documentation

| File | What it is for |
|---|---|
| [MANUAL.md](MANUAL.md) | Install and use it on Linux, macOS or Windows |
| This README | Reference: configuration, keys, discovery |
| [ARCHITECTURE.md](ARCHITECTURE.md) | How it is built and why |
| [docs/SECURITY_REVIEW.md](docs/SECURITY_REVIEW.md) | Audit findings and their status |
| [docs/CODEMAP.md](docs/CODEMAP.md) | One line per file |
| [docs/TODO.md](docs/TODO.md) | Known bugs and next steps |

## License

Internal tool — Nutanix proprietary.
