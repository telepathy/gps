# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GPS (Global Publishing System / 全局版本发布系统) orchestrates automated version releases across **Java multi-project, multi-module** codebases with complex cross-repository dependencies. It coordinates four external systems to release modules in correct topological order.

The design document (`design.md`) is the source of truth for all architectural decisions and is written in Chinese.

## Build & Run

```bash
go build -o gps-server .    # Build single binary (frontend embedded via //go:embed)
./gps-server                 # Start server at http://localhost:4777
go run main.go               # Build and run in one step
```

No separate frontend build step needed — static files are embedded at compile time.

### Auth config (optional environment variables)

```bash
GPS_JWT_SECRET           # JWT signing secret; if unset an ephemeral one is generated (sessions reset on restart)
GPS_GITLAB_URL           # GitLab instance, e.g. https://gitlab.internal.com
GPS_GITLAB_APP_ID        # OAuth app ID; setting this enables GitLab SSO
GPS_GITLAB_APP_SECRET    # OAuth app secret
GPS_GITLAB_CALLBACK_URL  # Callback, points to /auth/gitlab/callback
```

When GitLab is not configured, login falls back to mock login with the built-in `admin` account.

## Architecture

### Backend (Go + Gin)

```
main.go                         # Gin server, //go:embed, auth wiring, route registration
internal/
├── model/
│   ├── model.go                # All domain structs (Silo, Repo, Module, ReleasePlan, etc.)
│   └── user.go                 # User, Role, GitlabUser, auth request structs + action/role consts
├── auth/
│   └── service.go              # GitLab OAuth2 (resty, InsecureSkipVerify) + HS256 JWT generate/parse
├── dalaran/
│   └── client.go               # Fetches silo/repo tree from dalaran GET /api/v1/silos; skips non-devops repos (devopsOpt=false); module info ignored
├── middleware/
│   └── auth.go                 # RequireAuth: JWT from Bearer header or cookie → gin context
├── mock/
│   ├── generator.go            # Synthesizes modules + dependency DAG for dalaran-sourced repos (silo/repo are NOT mocked)
│   ├── store.go                # Thread-safe in-memory store (sync.RWMutex), SSE pub/sub, users/roles + seeded admin
│   └── simulator.go            # Goroutine-based release simulator (4-phase state machine)
└── handler/
    ├── silo.go                 # GET /api/silos, repos, modules
    ├── repo.go                  # GET /api/repos (full list + can_edit), PUT /api/repos/:id/branch
    ├── plan.go                 # CRUD for release plans (write ops: create/release action + silo-scope checks)
    ├── release.go              # Execute, progress polling, SSE event stream, abort (release action + silo-scope)
    ├── history.go              # Historical release records
    ├── auth.go                 # Login page, mock-login, GitLab callback, logout, current-user
    ├── admin.go                # User/role management (requires manage action)
    └── rbac.go                 # currentUser / requireAction / canReleaseSilos helpers
```

### Frontend (Vanilla JS SPA)

```
static/
├── index.html                  # SPA shell with hash-based routing (#/), nav user area + admin link
├── css/style.css               # Dark theme, CSS variables, full component styles
├── js/
│   ├── app.js                  # Hash router + lifecycle; checkAuth() gate, role-based nav/route gating
│   ├── api.js                  # Fetch wrapper (credentials + 401→/auth/login) + SSE + auth/admin methods
│   ├── pages/
│   │   ├── plan-create.js      # Silo selector → module preview → create plan
│   │   ├── version-confirm.js  # Collapsible grouped table, inline version editing
│   │   ├── release-monitor.js  # DAG visualization + real-time logs + phase stepper (core page)
│   │   ├── release-history.js  # Historical release list
│   │   ├── repos.js            # Flat repo table: silo code, repo name (link → SSH-to-HTTPS web URL), release branch, action; silo-code filter; branch editable only when can_edit
│   │   └── admin.js            # User role & silo-scope management + batch import (admin only)
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
- **Repo-level versioning**: all modules in the same repo share one version (one Git tag), auto-incremented by patch bump (e.g. `1.2.3` → `1.2.4`), editable per-repo before confirmation
- **DAG is edge-driven**: dependency graph is a flat list of `(from, to)` module-ID tuples with no layer metadata; the frontend computes layout depth via longest-path BFS from roots
- **Topo-sort + concurrent pool** (not layer-based grouping): any module whose upstream deps are done can start immediately
- **SSE for real-time updates**: unidirectional push from simulator to browser, simpler than WebSocket
- **Silo/repo from dalaran, modules mocked**: silo & repo data is fetched from dalaran's `GET /api/v1/silos` at startup (`GPS_DALARAN_URL` is required — a missing config or failed fetch is fatal). Only repos with `devopsOpt=true` are kept; repo `Name` is derived from the URL's last segment and `ReleaseBranch` defaults to `main`. Modules and the dependency DAG are always synthesized GPS-side (fixed seed 42) since dalaran's module info is intentionally not used.
- **GitLab SSO, GPS-internal RBAC**: identity is read-only from a self-signed GitLab (username only, TLS skipped); roles/permissions are owned by GPS, decoupled from GitLab org/project permissions

## API Endpoints

All `/api/*` routes are behind the `RequireAuth` middleware (401 if no valid JWT). Write operations add role + silo-scope checks (see Auth & RBAC below). `/auth/*` routes are public.

### Auth & Users

| Method | Path | Auth | Purpose | Used By |
|--------|------|------|---------|---------|
| GET | /auth/login | public | Standalone login page (GitLab button + mock-login form) | browser |
| POST | /auth/mock-login | public | Login by username (built-in `admin`, no GitLab) | login page |
| GET | /auth/gitlab/callback | public | GitLab OAuth2 callback → JWT cookie | GitLab |
| GET | /api/current-user | login | Current user (with roles) | app.js checkAuth |
| POST | /api/logout | login | Clear session cookie | nav logout |
| GET | /api/admin/users | manage | List users | admin page |
| GET | /api/admin/roles | manage | List roles | admin page |
| POST | /api/admin/users/import | manage | Batch pre-register users (`{users:[{username,email,roles,allowed_silos}]}`) → `{created,skipped,failed}` | admin page |
| PUT | /api/admin/users/:uid/roles | manage | Set a user's roles | admin page |
| PUT | /api/admin/users/:uid/access | manage | Set a user's `allowed_silos` | admin page |

**Auth & RBAC model**: Identity comes from a self-signed GitLab instance via OAuth2 (`scope=read_user`, TLS verification skipped, only `username` consumed); first-time GitLab users get the `viewer` role. Session is an HS256 JWT (24h) in an HttpOnly cookie. Three built-in roles map to actions: `admin` (view+create+release+manage, bypasses silo-scope), `releaser` (view+create+release within `allowed_silos`), `viewer` (view only). `allowed_silos` is `"*"` / `""` / comma-separated silo IDs. Users/roles live in the in-memory store (seeded `admin` with `allowed_silos="*"`); lost on restart.

**User import (pre-register + SSO bind)**: Admins can batch pre-register usernames (with optional roles/`allowed_silos`); imported users have `GitlabID=0`. Identity is still managed by GitLab SSO — on first GitLab login `FindOrCreateUser` matches by username, binds the GitLab info (id/email/avatar) and **preserves the imported roles & silo scope**. Re-importing an existing username is skipped (never overwrites). Empty roles default to `viewer`; unknown roles are rejected.

### Product Tree

| Method | Path | Purpose | Used By |
|--------|------|---------|---------|
| GET | /api/silos | List all silos (from dalaran) | plan-create |
| GET | /api/silos/:id/repos | Repos under a silo | plan-create |
| GET | /api/repos/:id/modules | Modules under a repo | plan-create |

### Repos

| Method | Path | Auth | Purpose | Used By |
|--------|------|------|---------|---------|
| GET | /api/repos | login | All repos as `RepoView` (silo_name + per-user `can_edit`) | repos page |
| PUT | /api/repos/:id/branch | release + silo-scope | Set a repo's release branch (`{release_branch}`) | repos page |

`can_edit` is true when the user has the `release` action **and** the repo's silo is within the user's `allowed_silos`. Viewing the full list only requires login; the branch write is hard-checked server-side (403 otherwise).

### Release Plans

| Method | Path | Purpose | Used By |
|--------|------|---------|---------|
| POST | /api/plans | Create release plan (auto-computes patch-bump versions per repo) | plan-create |
| GET | /api/plans | List all plans | plans list page |
| GET | /api/plans/:id | Plan detail with modules and dep_graph | version-confirm, release-monitor |
| PUT | /api/plans/:id/versions | Override target versions by repo_id (`{versions: {repo_id: "x.y.z"}}`) | version-confirm |
| POST | /api/plans/:id/confirm | Confirm plan, triggers topo-sort to build dep_graph | version-confirm |

### Release Execution

| Method | Path | Purpose | Used By |
|--------|------|---------|---------|
| POST | /api/plans/:id/execute | Start async execution | version-confirm |
| GET | /api/plans/:id/progress | Poll execution progress (stats + per-module status) | release-monitor |
| GET | /api/plans/:id/events | SSE real-time event stream | release-monitor |
| POST | /api/plans/:id/abort | Abort running release | release-monitor |
| POST | /api/plans/:id/modules/:mid/retry | Retry a single failed module | release-monitor |

### History

| Method | Path | Purpose | Used By |
|--------|------|---------|---------|
| GET | /api/history | List completed releases (newest first) | release-history |
| GET | /api/history/:id | Historical release detail (full plan data) | release-history → monitor |

### SSE Event Types (GET /api/plans/:id/events)

| type | data | Purpose |
|------|------|---------|
| phase_change | `{phase}` | Phase transition: TAGGING → ANALYZING → RELEASING → COMPLETED |
| module_status | `{module_id, status, error_msg?}` | Module state change, drives DAG node colors |
| module_log | `{module_id, line, timestamp}` | Build log line, appended to log panel |
| plan_complete | `{status, succeeded, failed, skipped}` | Release finished, stops SSE and polling |

## External System Interfaces

GPS is a **coordination layer** — it does not directly perform Git operations, build code, or manage artifacts. All actual work is delegated to four external systems. Below defines the boundary: which data GPS owns vs. which it fetches/pushes externally, and the interface contracts for each external system.

### System Boundary Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     GPS (本系统)                              │
│                                                             │
│  Owns:  ReleasePlan, execution state, phase orchestration,  │
│         concurrency pool, failure strategy, SSE push,       │
│         version override (human input), topo-sort result    │
│                                                             │
│  Does NOT own:  product tree data, source code, Git repos,  │
│         dependency relationships, build/test execution,     │
│         artifact publishing, artifact version registry      │
└────┬──────────┬──────────────┬──────────────┬───────────────┘
     │          │              │              │
     ▼          ▼              ▼              ▼
   PTIS        DAS           CI/CD          DMS
 产品树信息   依赖分析系统    发版流水线     依赖管理系统
```

### Phase-to-External-System Mapping

| Release Phase | External System Called | Purpose |
|---------------|----------------------|---------|
| Phase 0 — Preparation | PTIS (read) | Fetch silo→repo→module tree, current versions |
| Phase 1 — Tagging | PTIS (write, Git ops) | Clone repo, create tag, push |
| Phase 2 — Dep Analysis | DAS (read) | Fetch dependency edges for module set |
| Phase 3 — Pool Release | CI/CD (trigger + poll) | Trigger per-module pipeline, poll status |
| Phase 3 — Pool Release (on success) | DMS (write) | Register new artifact version |
| Module Retry | CI/CD (trigger + poll) | Re-trigger pipeline for failed module |

---

### PTIS — Product Tree Info System (产品树信息系统)

Maintains the silo→repo→module hierarchy, Git repo metadata, and version history.

#### PTIS-1: Get Product Tree

```
GPS → PTIS
GET /ptis/api/v1/silos/{silo_id}/tree

Response 200:
{
  "silo": {
    "id": "silo-001",
    "name": "payment",
    "desc": "支付核心服务"
  },
  "repos": [
    {
      "id": "repo-001",
      "name": "payment-core",
      "url": "git@gitlab.internal.com:platform/payment-core.git",
      "release_branch": "release/2025Q2",
      "modules": [
        { "id": "mod-001", "name": "payment-core-model" },
        { "id": "mod-002", "name": "payment-core-api" }
      ]
    }
  ]
}
```

**GPS mock mapping**: `GET /api/silos` + `GET /api/silos/:id/repos` + `GET /api/repos/:id/modules`

#### PTIS-2: Get Latest Released Version

```
GPS → PTIS
GET /ptis/api/v1/repos/{repo_id}/version?branch={release_branch}

Response 200:
{
  "repo_id": "repo-001",
  "version": "1.2.3",
  "released_at": "2025-04-10T08:30:00Z"
}
```

**GPS mock mapping**: Embedded in `Module.CurrentVersion` field at data generation time. Same version for all modules in one repo.

#### PTIS-3: Create Tag

```
GPS → PTIS
POST /ptis/api/v1/repos/{repo_id}/tags

Request:
{
  "branch": "release/2025Q2",
  "tag": "v1.2.4",
  "message": "Release v1.2.4 by GPS plan-001"
}

Response 200:
{
  "repo_id": "repo-001",
  "tag": "v1.2.4",
  "commit_sha": "abc123..."
}
```

**GPS mock mapping**: Simulated in `simulator.phaseTagging()` — sets modules to TAGGED status after 300-800ms per repo.

#### PTIS-4: Record New Version

```
GPS → PTIS
POST /ptis/api/v1/repos/{repo_id}/versions

Request:
{
  "version": "1.2.4",
  "plan_id": "plan-001",
  "released_at": "2025-05-13T10:00:00Z"
}

Response 200:
{ "status": "recorded" }
```

**GPS mock mapping**: Not explicitly mocked. Would be called after each module's successful release in Phase 3.

---

### DAS — Dependency Analysis System (依赖分析系统)

Analyzes inter-module dependency relationships from Gradle build files. Returns a flat list of `(from, to)` tuples — no layers, no grouping.

#### DAS-1: Get Dependency Graph

```
GPS → DAS
POST /das/api/v1/dependencies/analyze

Request:
{
  "module_ids": ["mod-001", "mod-002", "mod-003", ...],
  "branch": "release/2025Q2"
}

Response 200:
{
  "edges": [
    { "from": "mod-001", "to": "mod-003" },
    { "from": "mod-001", "to": "mod-005" },
    { "from": "mod-002", "to": "mod-005" }
  ]
}
```

Semantics: `from` is depended upon by `to` — i.e. `to` depends on `from`, so `from` must be released before `to`.

**GPS mock mapping**: `generator.generateEdges()` produces these at startup; `store.ConfirmPlan()` filters to plan scope, runs `TopologicalSort()`, and stores the result in `plan.DepGraph`.

---

### CI/CD — Release Pipeline (发版流水线)

Executes the actual build/test/publish for a single module. GPS triggers the pipeline and polls for completion.

#### CICD-1: Trigger Pipeline

```
GPS → CI/CD
POST /cicd/api/v1/pipelines

Request:
{
  "module_id": "mod-003",
  "repo_url": "git@gitlab.internal.com:platform/payment-core.git",
  "branch": "release/2025Q2",
  "tag": "v1.2.4",
  "version": "1.2.4"
}

Response 200:
{
  "pipeline_id": "pipe-20250513-mod003-001",
  "status": "PENDING"
}
```

**GPS mock mapping**: Simulated in `simulator.releaseModule()` — sets module to RELEASING, emits log lines with 400-800ms intervals.

#### CICD-2: Poll Pipeline Status

```
GPS → CI/CD
GET /cicd/api/v1/pipelines/{pipeline_id}/status

Response 200:
{
  "pipeline_id": "pipe-20250513-mod003-001",
  "status": "RUNNING",          // PENDING | RUNNING | SUCCESS | FAILED
  "started_at": "2025-05-13T10:01:00Z",
  "finished_at": null,
  "error_message": null,
  "logs_url": "https://ci.internal.com/pipelines/12345/logs"
}
```

Status transitions: `PENDING → RUNNING → SUCCESS` or `PENDING → RUNNING → FAILED`

**GPS mock mapping**: Simulated inline — `releaseModule()` walks through log lines, randomly decides success/failure, then broadcasts `module_status` SSE events.

#### CICD-3: Retry Pipeline (same as trigger)

Module retry reuses CICD-1 with the same parameters. The CI/CD system creates a new pipeline_id.

**GPS mock mapping**: `simulator.RetryModule()` → calls `releaseModule()` with `maxRetries=0`.

---

### DMS — Dependency Management System (依赖管理系统)

Manages artifact version coordinates in Maven/Gradle repositories (Nexus, Artifactory, or an internal version registry).

#### DMS-1: Register Artifact Version

```
GPS → DMS
POST /dms/api/v1/artifacts/{module_id}/versions

Request:
{
  "module_id": "mod-003",
  "version": "1.2.4",
  "branch": "release/2025Q2",
  "artifact_coords": {
    "group_id": "com.platform.payment",
    "artifact_id": "payment-core-model",
    "packaging": "jar"
  }
}

Response 200:
{ "status": "registered" }
```

Called after each module's pipeline succeeds in Phase 3. This allows downstream modules to resolve the newly published version during their own build.

**GPS mock mapping**: Not explicitly mocked. Would be called between a module's SUCCESS status and unblocking its downstream dependents.

#### DMS-2: Query Current Version (optional)

```
GPS → DMS
GET /dms/api/v1/artifacts/{module_id}/versions/latest?branch={branch}

Response 200:
{
  "module_id": "mod-003",
  "version": "1.2.3",
  "branch": "release/2025Q2",
  "published_at": "2025-04-10T08:35:00Z"
}
```

Optional pre-release check. Could be used to verify DMS state matches PTIS before starting.

**GPS mock mapping**: Not mocked. Version info comes from PTIS (Module.CurrentVersion).

---

### GPS Internal vs External — Summary

| Data | Owner | GPS Reads From | GPS Writes To |
|------|-------|---------------|---------------|
| Silo/Repo/Module hierarchy | PTIS | Phase 0 | — |
| Git repo URL, release branch | PTIS | Phase 0 | — |
| Current released version (per repo) | PTIS | Phase 0 | Phase 3 (record new) |
| Git tags | PTIS (Git) | — | Phase 1 (create tag) |
| Module dependency edges `(from, to)` | DAS | Phase 2 | — |
| Topo-sorted release order | **GPS** | — | — |
| ReleasePlan, version overrides | **GPS** | — | — |
| Execution state, phase, concurrency | **GPS** | — | — |
| Build/test/publish execution | CI/CD | Phase 3 (poll) | Phase 3 (trigger) |
| Artifact version registry | DMS | — | Phase 3 (register) |
| SSE events, progress stats | **GPS** | — | — |
| Failure strategy, retry decisions | **GPS** | — | — |

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
