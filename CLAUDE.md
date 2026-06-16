# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GPS (Global Publishing System / 全局版本发布系统) orchestrates automated version releases across **Java multi-project, multi-module** codebases with complex cross-repository dependencies. It coordinates four external systems to release modules in correct topological order.

The design document (`design.md`) is the source of truth for all architectural decisions and is written in Chinese.

## Build & Run

Requires **Go 1.25+**. Uses Gin web framework and embeds the frontend via `//go:embed`.

```bash
go build -o gps-server .    # Build single binary (frontend embedded)
./gps-server                 # Start server at http://localhost:4777
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
    ├── release.go              # Execute, progress polling, SSE event stream, abort, retry
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

### External Systems (currently mocked)

GPS coordinates four external systems (all mocked in `internal/mock/`):
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
- **Repo-level versioning**: all modules in the same repo share one version (one Git tag), auto-incremented by patch bump (e.g. `1.2.3` → `1.2.4`), editable per-repo before confirmation
- **DAG is edge-driven**: dependency graph is a flat list of `(from, to)` module-ID tuples with no layer metadata; the frontend computes layout depth via longest-path BFS from roots
- **Topo-sort + concurrent pool** (not layer-based grouping): any module whose upstream deps are done can start immediately
- **SSE for real-time updates**: unidirectional push from simulator to browser, simpler than WebSocket
- **Mock data uses fixed seed (42)**: deterministic, reproducible data generation

## API Routes (defined in `main.go`)

All routes are under `/api`. The handlers delegate to `mock.Store` (thread-safe in-memory store) and `mock.Simulator` (goroutine-based release execution).

| Group | Routes | Handler |
|-------|--------|---------|
| Product tree | `GET /api/silos`, `/api/silos/:id/repos`, `/api/repos/:id/modules` | `SiloHandler` |
| Plans | `POST/GET /api/plans`, `GET /api/plans/:id`, `PUT /api/plans/:id/versions`, `POST /api/plans/:id/confirm` | `PlanHandler` |
| Execution | `POST /api/plans/:id/execute`, `GET /api/plans/:id/progress`, `GET /api/plans/:id/events` (SSE), `POST /api/plans/:id/abort`, `POST /api/plans/:id/modules/:mid/retry` | `ReleaseHandler` |
| History | `GET /api/history`, `GET /api/history/:id` | `HistoryHandler` |

SSE event types: `phase_change`, `module_status`, `module_log`, `plan_complete`

## External System Interfaces

GPS is a **coordination layer** — it does not directly perform Git operations, build code, or manage artifacts. All actual work is delegated to four external systems. See `design.md` §5 for full interface contracts.

```
┌─────────────────────────────────────────────────────────────┐
│                     GPS (本系统)                              │
│  Owns:  ReleasePlan, execution state, phase orchestration,  │
│         concurrency pool, failure strategy, SSE push,       │
│         version override (human input), topo-sort result    │
│  Does NOT own:  product tree, source code, Git repos,       │
│         dependency relationships, build/test, artifacts     │
└────┬──────────┬──────────────┬──────────────┬───────────────┘
     ▼          ▼              ▼              ▼
   PTIS        DAS           CI/CD          DMS
```

| Phase | External System | Purpose |
|-------|----------------|---------|
| Phase 0 — Preparation | PTIS (read) | Fetch silo→repo→module tree, current versions |
| Phase 1 — Tagging | PTIS (write, Git ops) | Clone repo, create tag, push |
| Phase 2 — Dep Analysis | DAS (read) | Fetch dependency edges for module set |
| Phase 3 — Pool Release | CI/CD (trigger + poll) | Trigger per-module pipeline, poll status |
| Phase 3 — Pool Release (on success) | DMS (write) | Register new artifact version |

## GPS Internal Data Model (DDL)

GPS 自身持久化的数据结构。当前原型使用内存存储，以下以关系型 DDL 表达未来落库的目标 schema。

### Enums

```sql
CREATE TYPE plan_status      AS ENUM ('DRAFT','CONFIRMED','RUNNING','COMPLETED','ABORTED');
CREATE TYPE release_phase    AS ENUM ('NONE','TAGGING','ANALYZING','RELEASING','COMPLETED');
CREATE TYPE module_status    AS ENUM ('PENDING','TAGGED','RELEASING','SUCCESS','FAILED','SKIPPED','RETRYING');
CREATE TYPE failure_strategy AS ENUM ('ABORT','SKIP','RETRY');
```

### release_plan — 发布计划主表

一次发版的全量配置和运行状态。

```sql
CREATE TABLE release_plan (
    id               VARCHAR(64)      PRIMARY KEY,
    silo_ids         TEXT[]            NOT NULL,           -- 本次发版涉及的竖井 ID 列表
    dms_branch       VARCHAR(128)     NOT NULL,           -- DMS 依赖分支 (如 release/2025Q2)
    concurrency      INT              NOT NULL DEFAULT 4, -- 并发池大小
    failure_strategy failure_strategy  NOT NULL DEFAULT 'ABORT',
    max_retries      INT              NOT NULL DEFAULT 3,
    status           plan_status      NOT NULL DEFAULT 'DRAFT',
    phase            release_phase    NOT NULL DEFAULT 'NONE',
    created_at       TIMESTAMPTZ      NOT NULL DEFAULT now(),
    started_at       TIMESTAMPTZ,                         -- Phase 1 开始时间
    completed_at     TIMESTAMPTZ                          -- 发布完成时间
);
```

### plan_module — 计划内模块条目

记录每个模块在本次发版中的版本、状态和执行信息。同一 repo 下的模块共享 prev_version / target_version。

```sql
CREATE TABLE plan_module (
    plan_id        VARCHAR(64)    NOT NULL REFERENCES release_plan(id),
    module_id      VARCHAR(64)    NOT NULL,  -- 来自 PTIS 的模块 ID
    module_name    VARCHAR(256)   NOT NULL,
    repo_id        VARCHAR(64)    NOT NULL,  -- 来自 PTIS 的仓库 ID
    repo_name      VARCHAR(256)   NOT NULL,
    silo_id        VARCHAR(64)    NOT NULL,
    silo_name      VARCHAR(128)   NOT NULL,
    prev_version   VARCHAR(32)    NOT NULL,  -- 仓库级：当前已发布版本
    target_version VARCHAR(32)    NOT NULL,  -- 仓库级：目标发布版本 (patch bump 或人工覆盖)
    is_overridden  BOOLEAN        NOT NULL DEFAULT FALSE, -- 是否人工修改了版本
    status         module_status  NOT NULL DEFAULT 'PENDING',
    pipeline_id    VARCHAR(128),             -- CI/CD 流水线 ID
    start_time     TIMESTAMPTZ,             -- 模块开始发布时间
    end_time       TIMESTAMPTZ,             -- 模块发布完成时间
    error_msg      TEXT,                     -- 失败时的错误信息
    retry_count    INT            NOT NULL DEFAULT 0,

    PRIMARY KEY (plan_id, module_id)
);

CREATE INDEX idx_plan_module_repo ON plan_module(plan_id, repo_id);
CREATE INDEX idx_plan_module_status ON plan_module(plan_id, status);
```

### plan_dep_edge — 计划内依赖边

本次发版范围内的模块间依赖关系，从 DAS 获取后存入。纯 `(from, to)` 二元组。

```sql
CREATE TABLE plan_dep_edge (
    plan_id    VARCHAR(64)  NOT NULL REFERENCES release_plan(id),
    from_id    VARCHAR(64)  NOT NULL,  -- 被依赖方 (上游模块)
    to_id      VARCHAR(64)  NOT NULL,  -- 依赖方 (下游模块)

    PRIMARY KEY (plan_id, from_id, to_id)
);

CREATE INDEX idx_dep_edge_to ON plan_dep_edge(plan_id, to_id);
```

### plan_topo_order — 拓扑排序结果

GPS 根据 plan_dep_edge 计算的全序发布序列。

```sql
CREATE TABLE plan_topo_order (
    plan_id    VARCHAR(64)  NOT NULL REFERENCES release_plan(id),
    seq        INT          NOT NULL,  -- 序号 (0-based)
    module_id  VARCHAR(64)  NOT NULL,

    PRIMARY KEY (plan_id, seq)
);
```

### release_history — 发布历史摘要

每次发布完成后写入的汇总记录，用于列表展示。

```sql
CREATE TABLE release_history (
    plan_id       VARCHAR(64)   PRIMARY KEY REFERENCES release_plan(id),
    silo_ids      TEXT[]        NOT NULL,
    silo_names    TEXT[]        NOT NULL,
    status        plan_status   NOT NULL,
    total_modules INT           NOT NULL,
    succeeded     INT           NOT NULL DEFAULT 0,
    failed        INT           NOT NULL DEFAULT 0,
    skipped       INT           NOT NULL DEFAULT 0,
    duration      VARCHAR(32),            -- 人类可读耗时 (如 "5m30s")
    created_at    TIMESTAMPTZ   NOT NULL,
    completed_at  TIMESTAMPTZ
);

CREATE INDEX idx_history_created ON release_history(created_at DESC);
```

### ER Relationships

```
release_plan  1 ──── N  plan_module      (plan_id)
release_plan  1 ──── N  plan_dep_edge    (plan_id)
release_plan  1 ──── N  plan_topo_order  (plan_id)
release_plan  1 ──── 1  release_history  (plan_id)
```

### Notes

- **外部实体不落 GPS 库**：Silo、Repo、Module 的主数据在 PTIS，GPS 只在 `plan_module` 中冗余快照 name/version 等字段，发版期间不再回查 PTIS。
- **依赖边是计划级快照**：`plan_dep_edge` 是 DAS 在 Phase 2 返回的当次快照，不同计划可能因分支不同而产生不同的依赖图。
- **版本号是仓库粒度**：同 `repo_id` 下所有 `plan_module` 行的 `prev_version` 和 `target_version` 相同，由仓库级 tag 决定。
- **pipeline_id 来自 CI/CD**：GPS 触发流水线后记录返回的 pipeline_id，用于后续状态轮询和日志链接。
