# NavForge

Vision-aware warehouse routing decision support, following [the HLD](docs/HLD.md). This first build includes a Go API, temporal occupancy engine, constrained Dijkstra planner, SQLite audit trail, simulated forklift and Next.js dashboard.

The actual PipeGen adapter and GPU pipeline are pending. No synthetic producer is connected to the application. The supplied warehouse and camera polygons are **unverified examples**; all lanes start unknown. This is a simulation and does not control machinery.

## Run locally on Windows

Requires Go 1.26+ and Node.js 22+. In PowerShell, from the repository root:

```powershell
$env:GOMODCACHE = Join-Path (Get-Location) 'data/go-mod'
$env:GOCACHE = Join-Path (Get-Location) 'data/go-cache'
go mod download
go run ./cmd/navforge
```

In a second terminal:

```powershell
cd web
npm ci --cache ../data/npm-cache
npm run dev
```

Open <http://127.0.0.1:3000>. The dashboard proxies API requests to `127.0.0.1:8080`. Choose start and destination, then start a simulated mission. Unknown lanes carry a configurable penalty by default; set `policy.strict_unknown` to `true` to exclude them. The map coordinates are drawing coordinates; edge lengths are separate example distances in meters.

The Go build caches are inside ignored `data/` to support this workspace's filesystem restrictions. On WSL, the same Go commands work with normal cache settings, followed by `cd web && npm ci && npm run dev`.

## Connect real perception

Follow [the integration guide](docs/INTEGRATION.md). Set a private ingestion token of at least 24 characters in the backend terminal **before** launching Go. For example, generate a random token without printing it:

```powershell
$tokenBytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($tokenBytes)
$env:NAVFORGE_INGEST_TOKEN = [Convert]::ToBase64String($tokenBytes)
$rng.Dispose()
go run ./cmd/navforge
```

Pass that same token securely to the eventual local bridge. With no valid token configured, the ingestion listener is disabled. It is never exposed through the dashboard. Both listeners bind IPv4 loopback. A container/WSL bridge must share the backend network namespace or use a deliberate local forwarding arrangement; Docker networking has not been verified yet.

## Checks

```powershell
go test ./...
go vet ./...
cd web
npm run typecheck
npm run build
```

The Go tests use synthetic fixtures and temporary databases to cover routing constraints, geometry, hysteresis, stale input, causal replanning, duplicates, retired sessions, restart behavior, transaction failure and HTTP ingestion boundaries. Those fixtures are not inference evidence. `npm run build` compiles and checks the dashboard; browser interaction has not yet been visually verified.

## Project layout

| Path | Purpose |
| --- | --- |
| `cmd/navforge` | API and ingestion server, shutdown and simulator ticks |
| `internal/nav` | Contracts, planner, occupancy, service, SQLite and tests |
| `configs/warehouse.json` | Example map, vehicle, polygons and configurable policy |
| `web` | Next.js / TypeScript operations dashboard |
| `docs/INTEGRATION.md` | Frame contract and pending PipeGen handoff |
| `pipegen/export` | Reserved location for real vendor-generated exports |
| `evidence/manifest.json` | Explicitly pending hackathon evidence |

API: `GET /health`, `/api/map`, `/api/state`, `/api/occupancy`, `/api/routes/current`, `/api/events?limit=100`, `/api/stream` (SSE), and `POST /api/missions`. Authenticated bridge frames go to `POST http://127.0.0.1:8081/internal/perception/events`.

SQLite writes observations, state changes and resulting route decisions in one transaction. Duplicates and retired sessions remain rejected after restart. Perception and missions reset on restart; historical events remain, and route/graph versions continue. Event timestamps are server receive/commit time; occupancy durations use video timestamps and stale detection uses server time. The dashboard's JSON download contains the latest 1,000 events at most; the complete SQLite database is under `data/`.

Simulation limitation: a replan during an edge traversal resets the virtual forklift to its last reached waypoint and records that in the route explanation. MP4 preview is local, unsynchronized playback and performs no inference. ETA uses example speed limits and excludes turn delays. Measured inference FPS, occupancy accuracy, replan latency, real calibration, PipeGen exports and GPU evidence remain pending.
