# ntnx-topo Architecture Document — v1 (superseded)

> **Status:** superseded. This describes the single-pane dashboard as it stood up to 2026-04:
> one cluster at a time, one vertical CVM/host/switch drawing, no Inventory or Config pane,
> no PC/AZ graph, no mouse support and no tests.
> **Current document:** [../ARCHITECTURE.md](../ARCHITECTURE.md) (v2).
> Still accurate here: section 6 (platform compatibility) and the cross-compilation commands.

## 1. Directory Structure

```
~/Documents/ntnx-topo/
├── cmd/
│   └── app/
│       └── main.go                 # Entrypoint — wires config, fetcher, and UI
├── internal/
│   ├── config/
│   │   └── config.go               # YAML + env + CLI flag config loading
│   ├── model/
│   │   ├── topology.go             # Domain types: Node, Edge, Cluster, NodeRole, Status
│   │   └── state.go                # Thread-safe AppState + copy-safe StateSnapshot
│   ├── api/
│   │   ├── client.go               # Shared BaseClient: HTTP, auth, JSON, error types
│   │   ├── v2/
│   │   │   ├── endpoints.go        # Prism Element v2 endpoint paths + response structs
│   │   │   └── client.go           # v2 REST client: GET /clusters, /hosts
│   │   ├── v3/
│   │   │   ├── endpoints.go        # Prism Central v3 endpoint paths + response structs
│   │   │   └── client.go           # v3 REST client: POST /clusters/list, /hosts/list
│   │   └── v4/
│   │       ├── endpoints.go        # v4 API version constant
│   │       └── client.go           # v4 client wrapping Nutanix official Go SDK
│   ├── fetcher/
│   │   └── fetcher.go              # Background polling, v4->v3->v2 fallback, channel publishing
│   └── ui/
│       ├── styles.go               # Lipgloss v2 color/style definitions
│       ├── tui.go                  # Bubbletea v2 Model: Init, Update, View
│       └── view.go                 # ASCII topology renderer with dynamic coloring
├── config.yaml                     # Sample configuration file
├── go.mod / go.sum                 # Go module definition
├── .gitignore
└── README.md
```

### Why This Layout

The project follows the standard Go project layout convention:

- `cmd/` holds the executable entrypoint only -- no business logic.
- `internal/` prevents external packages from importing internals.
- Each sub-package has a single responsibility (config, model, api, fetcher, ui).
- The API clients are versioned into `v2/`, `v3/`, `v4/` sub-packages because they
  have completely different wire protocols and response shapes.

---

## 2. System Design

The application has four layers arranged in a strict dependency chain:

```
┌──────────────────────────────────────────────────────┐
│                    cmd/app/main.go                    │
│         (wires everything, starts goroutines)         │
└──────────┬────────────────────┬──────────────────────┘
           │                    │
           ▼                    ▼
┌─────────────────┐   ┌────────────────────┐
│   fetcher.go    │   │      ui/tui.go     │
│  (goroutine)    │──>│   (Bubbletea loop) │
│                 │ ch│                    │
└────────┬────────┘   └────────────────────┘
         │                      │
         ▼                      ▼
┌─────────────────┐   ┌────────────────────┐
│  api/v4, v3, v2 │   │  ui/view.go        │
│  (HTTP clients) │   │  ui/styles.go      │
└────────┬────────┘   └────────────────────┘
         │
         ▼
┌─────────────────┐
│   model/        │
│ topology, state │
└─────────────────┘
```

**Data flow in one sentence:** The fetcher goroutine polls Nutanix APIs, normalizes
responses into a `Cluster` topology, wraps it in a `StateSnapshot`, and sends it
over a channel to the Bubbletea event loop, which re-renders the terminal.

---

## 3. How Each File Works

### cmd/app/main.go

The glue. Sets up structured logging to a file (so `slog` output doesn't corrupt
the TUI), loads config, creates the buffered channel, creates the fetcher, launches
the fetcher goroutine, creates the TUI model, and runs `tea.NewProgram(m).Run()`.

### internal/config/config.go

Loads configuration with a three-tier override chain:
  1. YAML file (base defaults)
  2. Environment variables (`NTNX_PC_IP`, `NTNX_PE_IP`, `NTNX_USER`, `NTNX_PASS`)
  3. CLI flags (`--pc-ip`, `--pe-ip`, `--config`) -- highest priority

Returns a `Config` struct consumed by the rest of the app. Validates that at least
one Prism IP is set.

### internal/model/topology.go

Defines the normalized domain model:
- `Node` -- with ID, name, IP, status, role
- `Edge` -- from/to relationship
- `Cluster` -- name, list of nodes, list of connections, hasPC flag

Has helper methods `CVMs()`, `Hosts()`, `PC()`, `TOR()` that filter nodes by role,
so the view layer can easily extract each tier of the topology.

### internal/model/state.go

Two types with different purposes:

- `AppState` -- lives inside the fetcher goroutine, protected by `sync.RWMutex`.
  Has setter methods (`SetCluster`, `SetMeta`, `SetActiveFetch`) and a `Snapshot()`
  method that produces a deep copy of all slice fields.

- `StateSnapshot` -- a plain value type with no mutex. Safe to copy, send over
  channels, and read from the UI thread without any synchronization.

### internal/api/client.go

The shared HTTP foundation. `BaseClient` handles:
- HTTPS connections to port 9440
- HTTP Basic Auth headers
- JSON marshaling/unmarshaling via `DoJSON(method, path, body, result)`
- Classifies HTTP responses into `APIError` types with helper methods
  (`IsNotFound`, `IsUnauthorized`, `IsServerError`, `IsClientError`)
- `ClassifyStatus()` maps HTTP codes to health categories (healthy/warning/failure)

### internal/api/v2/client.go

Prism Element REST client. Calls:
- `GET /api/nutanix/v2.0/clusters/` -- cluster info
- `GET /api/nutanix/v2.0/hosts/` -- list hosts

Maps the flat JSON responses into the `model.Cluster` topology. Sets `HasPC = false`
since v2 is Prism Element only. Creates CVM nodes paired with each host, and a TOR
switch node connected to all hosts.

### internal/api/v3/client.go

Prism Central REST client. Calls:
- `POST /api/nutanix/v3/clusters/list` with `{"kind":"cluster"}`
- `POST /api/nutanix/v3/hosts/list` with `{"kind":"host"}`

Maps nested v3 response structures (with `metadata`, `spec`, `status` sub-objects)
into the topology. Adds a Prism Central node at the top of the topology. Extracts
CVM IP from the `controller_vm.ip` field nested in host resources.

### internal/api/v4/client.go

Wraps the official Nutanix Go SDK (`clustermgmt-go-client/v4`). Configures the SDK's
`ApiClient` with host/creds/TLS, then calls `ListClusters()` and `ListHosts()` on
the SDK APIs. Extracts IPv4 addresses from deeply nested SDK types
(`HypervisorReference.ExternalAddress.Ipv4.Value`). Maps `AcropolisConnectionState`
to UP/DOWN status, and checks `IsDegraded` for degraded status.

### internal/fetcher/fetcher.go

The polling orchestrator. Runs in its own goroutine with a `time.Ticker`. Each tick:

1. Sets `ActiveFetch = true` and publishes (UI shows "fetching..." indicator)
2. Runs the fallback chain: try v4, if that fails try v3, if that fails try v2
3. On complete failure, generates a gray/unknown placeholder topology
4. Sets the cluster data, latency, API version, and errors on AppState
5. Publishes the snapshot on the channel

All three API clients implement the `TopologyProvider` interface:
```go
type TopologyProvider interface {
    FetchTopology() (*model.Cluster, error)
}
```

The `ManualRefresh()` method sends a signal on a separate `refreshCh` that the
select loop picks up for an immediate out-of-cycle fetch.

### internal/ui/styles.go

Defines all Lipgloss v2 styles as package-level variables:
- Green/yellow/red/gray for status colors
- Cyan for the title
- Dim gray for connectors and help text
- White bold for metrics
- Rounded-border box style for component boxes

`StyleForStatus()` maps a status enum to a style, optionally adding blink when
an API call is active. `BoxForStatus()` renders a component name inside a
rounded-border box with status-colored border and text.

### internal/ui/tui.go

The Bubbletea v2 model. Implements the Elm Architecture:

- `Init()` -- starts two async commands: one blocks on the update channel waiting
  for snapshots, one sleeps 1 second for UI tick updates (refreshes timestamps).

- `Update()` -- handles four message types:
  - `tea.KeyPressMsg`: q/ctrl+c = quit, r = trigger manual refresh
  - `stateMsg`: stores the new snapshot and re-queues the channel listener
  - `tickMsg`: re-queues the 1-second ticker (updates "Last refresh: Xs ago")
  - `tea.WindowSizeMsg`: captures terminal dimensions for responsive layout

- `View()` -- delegates to `RenderTopology()` and enables alt-screen mode via
  `tea.View{AltScreen: true}`.

### internal/ui/view.go

Builds the full terminal output by composing sections vertically:

1. Title bar ("Nutanix Cluster: <name>")
2. Prism Central box (if configured) -- centered, color-coded
3. Connector lines (branching tree structure)
4. CVM row -- all CVMs side by side with IP addresses, color-coded
5. Vertical pipe connectors
6. Host row -- all hosts side by side with IP addresses, color-coded
7. Connector lines (converging)
8. TOR Switch box -- centered, color-coded
9. Metrics panel (API version, last refresh time, latency, fetch indicator)
10. Error line (last error message, if any)
11. Help bar (keyboard shortcuts)

Uses `lipgloss.Place()` for centering within terminal width, and
`lipgloss.JoinHorizontal/JoinVertical` for composition.

---

## 4. Concurrency Model

There are exactly **two goroutines** at runtime:

```
┌─────────────────────────┐          ┌─────────────────────────┐
│    Fetcher Goroutine    │          │   Main / Bubbletea      │
│                         │          │    Event Loop            │
│  ┌───────────────────┐  │  chan    │  ┌───────────────────┐  │
│  │ time.Ticker (5s)  │──│────────> │  │ waitForState()    │  │
│  │ refreshCh signal  │  │ State    │  │ blocks on <-ch    │  │
│  └───────────────────┘  │ Snapshot │  └───────────────────┘  │
│           │              │          │           │              │
│           v              │          │           v              │
│  ┌───────────────────┐  │          │  ┌───────────────────┐  │
│  │ fetchWithFallback  │  │          │  │ Update(stateMsg)  │  │
│  │ v4 -> v3 -> v2     │  │          │  │ stores snapshot   │  │
│  └───────────────────┘  │          │  └───────────────────┘  │
│           │              │          │           │              │
│           v              │          │           v              │
│  ┌───────────────────┐  │          │  ┌───────────────────┐  │
│  │ AppState (mutex)  │  │          │  │ View() -> render  │  │
│  │ Snapshot() -> copy│  │          │  │ reads StateSnapshot│  │
│  └───────────────────┘  │          │  └───────────────────┘  │
└─────────────────────────┘          └─────────────────────────┘
```

### Key Design Decisions

1. **Channel-based communication** -- The fetcher sends `StateSnapshot` values
   (plain structs, no pointers, no mutexes) on a buffered channel of size 1.
   The UI never directly touches the fetcher's internal state.

2. **Non-blocking publish** -- The `publish()` method uses a triple-select pattern:
   try to send, if full then drain one and retry. The fetcher never blocks waiting
   for the UI to consume.

3. **Snapshot isolation** -- `AppState.Snapshot()` does a deep copy of all slices
   (Nodes, Connections, Errors) under a read lock. The resulting `StateSnapshot`
   is completely independent -- the UI can read it without any synchronization.

4. **UI never blocks API calls** -- Bubbletea runs `waitForState()` as an async
   `tea.Cmd`. It blocks in its own goroutine managed by Bubbletea's runtime.
   When a snapshot arrives, it becomes a `stateMsg` dispatched to `Update()`.
   The main event loop stays responsive for key presses and window resizes.

5. **Manual refresh** -- When the user presses `r`, `Update()` calls
   `fetcher.ManualRefresh()` which sends on `refreshCh` (buffered size 1,
   non-blocking). The fetcher's select loop picks it up on the next iteration.

6. **Graceful shutdown** -- `main()` creates a `context.WithCancel`. When the
   Bubbletea program exits (user presses q), `defer cancel()` fires, which
   causes `<-ctx.Done()` in the fetcher's select loop to trigger, cleanly
   stopping the goroutine.

### API Fallback Flow

```
    Fetch Cycle Start
         │
         v
    Prism Central configured?
        / \
      Yes   No
      │      │
      v      v
   Try v4   Try v2 ──> fail ──> Unknown topology (gray)
      │                  ^
    fail                 │
      │                  │
      v                  │
   Try v3               │
      │                  │
    fail ────────────────┘
      │
   success at any point ──> Update state, publish snapshot
```

---

## 5. Scalability

| Aspect | Current Capability | Scaling Path |
|--------|-------------------|-------------|
| **Number of nodes** | Renders all nodes in a single horizontal row | For 20+ nodes, add horizontal scrolling or pagination by slicing `CVMs()`/`Hosts()` based on terminal width |
| **Number of clusters** | Shows one cluster | Add a cluster selector (tab/arrow keys) and a `[]Cluster` in state. The fetcher already receives all clusters from list endpoints |
| **Poll frequency** | Single ticker, sequential API calls | Parallelize v4 cluster + host calls with `errgroup`. For sub-second updates, switch to websocket/event streaming if supported |
| **Multi-user** | Single-user CLI | Bubbletea supports `tea.WithInput`/`tea.WithOutput` for SSH-served TUIs via the Wish library. Could serve the dashboard to multiple users over SSH |
| **Memory** | Holds one snapshot in memory | O(N) where N = number of nodes. Even at 1000 nodes, this is negligible (< 1 MB) |
| **Additional API versions** | v2, v3, v4 | Implement the `TopologyProvider` interface and add to the fallback chain in `fetcher.go` |

---

## 6. Platform Compatibility

| Platform | Status | Notes |
|----------|--------|-------|
| **macOS (arm64, amd64)** | Fully supported | Developed and tested here |
| **Linux (amd64, arm64)** | Fully supported | Go cross-compiles with `GOOS=linux`. Bubbletea/Lipgloss use POSIX terminal APIs |
| **Windows** | Mostly supported | Bubbletea v2 has Windows support via `charmbracelet/x/windows`. Works in Windows Terminal and modern PowerShell. Classic `cmd.exe` has limited ANSI support; some box-drawing characters may render incorrectly |
| **SSH / tmux** | Supported | Bubbletea detects terminal capabilities. Colors may downgrade to 256-color or 16-color depending on `TERM` |

### Cross-Compilation

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o ntnx-topo-linux ./cmd/app/

# Windows
GOOS=windows GOARCH=amd64 go build -o ntnx-topo.exe ./cmd/app/

# Linux ARM (e.g., Raspberry Pi, ARM servers)
GOOS=linux GOARCH=arm64 go build -o ntnx-topo-linux-arm64 ./cmd/app/
```

The only external dependency that could cause platform issues is the Nutanix Go SDK,
which uses `net/http` under the hood -- fully portable across all Go-supported platforms.

---

## 7. Pros

- **Clean separation of concerns** -- Each package has a single responsibility.
  The API clients know nothing about the UI. The UI knows nothing about HTTP.
  The fetcher is the only bridge.

- **Interface-based API layer** -- All three API clients implement
  `TopologyProvider` (single method: `FetchTopology() (*Cluster, error)`).
  This makes it trivial to add a v5 client, a mock client for testing, or a
  file-based client for offline use.

- **Graceful degradation** -- If all APIs fail, the app doesn't crash. It renders
  every component in gray with "unknown" status and shows the error in the UI.
  If v4 fails, it silently falls back to v3, then v2.

- **Zero shared mutable state between goroutines** -- The channel carries
  immutable value-type snapshots. No data races possible. Passes `go vet` and
  would pass the `-race` detector cleanly.

- **Structured logging** -- Uses Go 1.21+ `log/slog` with structured key-value
  fields. Logs go to a file so they never corrupt the TUI output.

- **Configuration flexibility** -- Three-tier config (file, env vars, CLI flags)
  means it works in dev (edit config.yaml), CI/automation (env vars), and
  ad-hoc use (flags).

- **Modern library choices** -- Bubbletea v2 and Lipgloss v2 are the latest (2026)
  versions using the new `charm.land` import path, `tea.View` struct pattern, and
  deterministic styling.

- **Single binary, no runtime dependencies** -- `go build` produces a static
  binary. No Python, no Node.js, no Docker required.

---

## 8. Cons

- **No unit tests** -- The codebase has no test files. The `TopologyProvider`
  interface makes testing straightforward (mock the interface, test the fetcher
  logic), but tests haven't been written yet.

- **No authentication caching** -- Each API call sends Basic Auth credentials.
  There's no session cookie reuse, which means slightly more overhead per request
  and repeated authentication on the server side.

- **Single-cluster view** -- The topology renderer assumes one cluster.
  Multi-cluster environments (common with Prism Central) would need a cluster
  selection screen or tabbed view.

- **Limited terminal width handling** -- If the terminal is narrow or there are
  many nodes, the horizontal layout can overflow. There's no scroll, truncation,
  or responsive layout breakpoint.

- **Hardcoded port 9440** -- The Nutanix API port is hardcoded in
  `BaseClient.DoJSON()` and the v4 SDK client setup. Non-standard port
  deployments would need a config field.

- **No TLS certificate pinning** -- `Insecure: true` is the default, skipping
  TLS verification. Fine for internal clusters, but not ideal for production
  security. There's no option to provide a custom CA certificate.

- **Blink effect depends on terminal** -- The "active fetch" glow uses ANSI blink,
  which many modern terminals ignore or disable. The bold effect works everywhere,
  but the blink may be invisible.

- **No persistent state** -- The app doesn't cache the last-known topology to disk.
  If it starts and all APIs are down, it shows a blank gray topology rather than
  the last-known-good state.
