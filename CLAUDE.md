# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GPS (Global Publishing System / 全局版本发布系统) orchestrates automated version releases across **Java multi-project, multi-module** codebases with complex cross-repository dependencies. It coordinates four external systems to release modules in correct topological order.

The design document (`design.md`) is the source of truth for all architectural decisions and is written in Chinese.

## Build & Run

```bash
go build -o gps-server .    # Build single binary (frontend embedded via //go:embed)
./gps-server                 # Start server at http://localhost:8080
go run main.go               # Build and run in one step
```

No separate frontend build step needed — static files are embedded at compile time.

## Architecture

### Backend (Go + Gin)

```
main.go                         # Gin server, //go:embed, route registration
internal/
├── model/model.go              # All domain structs (Silo, Repo, Module, ReleasePlan, etc.)
├── mock/
│   ├── generator.go            # Deterministic mock data (30 silos, 47 repos, 104 modules + DAG)
│   ├── store.go                # Thread-safe in-memory store (sync.RWMutex), SSE pub/sub
│   └── simulator.go            # Goroutine-based release simulator (4-phase state machine)
└── handler/
    ├── silo.go                 # GET /api/silos, repos, modules
    ├── plan.go                 # CRUD for release plans
    ├── release.go              # Execute, progress polling, SSE event stream, abort
    └── history.go              # Historical release records
```

### Frontend (Vanilla JS SPA)

```
static/
├── index.html                  # SPA shell with hash-based routing (#/)
├── css/style.css               # Dark theme, CSS variables, full component styles
├── js/
│   ├── app.js                  # Hash router + page lifecycle management
│   ├── api.js                  # Fetch wrapper + SSE subscription + utility functions
│   ├── pages/
│   │   ├── plan-create.js      # Silo selector → module preview → create plan
│   │   ├── version-confirm.js  # Collapsible grouped table, inline version editing
│   │   ├── release-monitor.js  # DAG visualization + real-time logs + phase stepper (core page)
│   │   └── release-history.js  # Historical release list
│   └── components/
│       ├── dag-graph.js        # Pure SVG DAG with pan/zoom (no D3 dependency)
│       └── log-panel.js        # Filterable real-time log panel
```

### External Systems (from design.md)

GPS coordinates four external systems (currently mocked):
- **PTIS** (Product Tree Info System): silo→repo→module hierarchy, release branches, version history
- **DAS** (Dependency Analysis System): inter-module dependency graph from Gradle
- **CI/CD** (Release Pipeline): per-module build/test/publish, polled via pipeline_id
- **DMS** (Dependency Management System): artifact version coordinates (Maven-style)

## Core Domain Concepts

**Product tree** has three levels: **Silo** → **Repository** → **Module**. Tags are per-repo; dependencies and pipeline triggers are per-module.

**Release flow** has four phases:
- **Phase 0** — Preparation: fetch product tree, auto-increment versions (SemVer patch bump), allow manual overrides
- **Phase 1** — Tagging: clone/tag/push per repo (repos in parallel)
- **Phase 2** — Dependency analysis: DAG topo-sort, cycle detection
- **Phase 3** — Concurrent pool release: configurable concurrency, upstream-ready gating, failure strategies (ABORT/SKIP/RETRY)
- **Phase 4** — Reporting

## Key Design Decisions

- **Repo-level tagging, module-level releasing**: avoids redundant Git operations
- **Topo-sort + concurrent pool** (not layer-based grouping): any module whose upstream deps are done can start immediately
- **SSE for real-time updates**: unidirectional push from simulator to browser, simpler than WebSocket
- **Mock data uses fixed seed (42)**: deterministic, reproducible data generation

## API Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | /api/silos | List all silos |
| GET | /api/silos/:id/repos | Repos under a silo |
| GET | /api/repos/:id/modules | Modules under a repo |
| POST | /api/plans | Create release plan |
| GET | /api/plans | List plans |
| GET | /api/plans/:id | Plan detail |
| PUT | /api/plans/:id/versions | Override module versions |
| POST | /api/plans/:id/confirm | Confirm plan (triggers topo-sort) |
| POST | /api/plans/:id/execute | Start async execution |
| GET | /api/plans/:id/progress | Poll execution progress |
| GET | /api/plans/:id/events | SSE real-time event stream |
| POST | /api/plans/:id/abort | Abort running release |
| GET | /api/history | List completed releases |
| GET | /api/history/:id | Historical release detail |
