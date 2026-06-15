# GPS — 全局版本发布系统

GPS（Global Publishing System）是一个面向 **Java Gradle 多仓库、多模块** 项目的集中式版本发布编排系统。它协调打 tag、依赖分析、拓扑排序、并发池发布等阶段，确保模块按正确的依赖顺序发布。

## 核心特性

- **GA 模块标识**：以 `group:artifact` 作为模块的全局规范主键，统一项目内（`project()`）和项目间（`g:a:v`）依赖
- **节点三分类**：internal（本次发布）/ pending-external（自研未 devops，需人工确认）/ third-party（第三方库，不进 DAG）
- **模块级 DAG**：拓扑排序、并发池调度、上游就绪判断均在模块粒度进行，避免 repo 级环问题
- **计划级数据隔离**：每次发版独立生成模块和依赖图，不跨计划共享
- **环检测**：确认计划时自动检测模块级依赖环并定位环路径
- **SSE 实时推送**：发版过程中的阶段变化、模块状态、构建日志通过 Server-Sent Events 实时推送到前端

## 快速开始

### 前置条件

- Go 1.25+
- MySQL 8.0（可选，不配置则使用内存存储）
- dalaran 实例（产品树数据源）

### 构建与运行

```bash
# 构建（前端静态文件自动嵌入，无需单独构建）
go build -o gps-server .

# 启动（内存模式）
GPS_DALARAN_URL=http://your-dalaran-host ./gps-server

# 启动（MySQL 持久化模式）
GPS_DALARAN_URL=http://your-dalaran-host \
GPS_MYSQL_DSN="root:password@tcp(127.0.0.1:3306)/gps?charset=utf8mb4&parseTime=True&loc=Local" \
./gps-server
```

服务默认监听 `http://localhost:4777`。

### Docker MySQL（本地开发）

```bash
docker compose up -d          # 启动 MySQL 8.0 容器
docker exec gps-mysql mysql -u root -pgps123456 gps  # 连接数据库
```

### 环境变量

#### 数据源

| 变量 | 必填 | 默认值 | 说明 |
|---|---|---|---|
| `GPS_DALARAN_URL` | ✅ | — | dalaran 实例地址，产品树（竖井→仓库）的唯一数据源。启动时拉取，失败则进程退出。 |
| `GPS_MYSQL_DSN` | — | — | MySQL 连接串（格式：`user:pass@tcp(host:port)/dbname?params`）。不设置则使用内存存储，重启丢失所有数据。 |

#### 认证

| 变量 | 必填 | 默认值 | 说明 |
|---|---|---|---|
| `GPS_JWT_SECRET` | — | 随机生成 | HS256 JWT 签名密钥。不设置则每次启动生成临时密钥（所有已登录用户的 session 在重启后失效）。设置固定值可保持 session 跨重启有效。 |
| `GPS_GITLAB_URL` | — | `https://gitlab.local` | GitLab 实例地址。仅在设置了 `GPS_GITLAB_APP_ID` 时生效。 |
| `GPS_GITLAB_APP_ID` | — | — | GitLab OAuth Application ID。**设置此项即启用 GitLab SSO**，登录页出现「使用 GitLab 账号登录」按钮。不设置则仅使用内置 `admin` 账号的 mock 登录。 |
| `GPS_GITLAB_APP_SECRET` | — | — | GitLab OAuth Application Secret。与 `GPS_GITLAB_APP_ID` 配合使用。 |
| `GPS_GITLAB_CALLBACK_URL` | — | — | OAuth 回调地址，应指向 `http(s)://<gps-host>/auth/gitlab/callback`。GitLab OAuth Application 中配置的回调地址必须与此一致。 |

#### GitLab SSO 配置示例

```bash
# 在 GitLab 中创建 OAuth Application：
#   - Name: GPS
#   - Redirect URI: http://localhost:4777/auth/gitlab/callback
#   - Scopes: read_user

export GPS_GITLAB_URL="https://gitlab.internal.com"
export GPS_GITLAB_APP_ID="your-app-id"
export GPS_GITLAB_APP_SECRET="your-app-secret"
export GPS_GITLAB_CALLBACK_URL="http://localhost:4777/auth/gitlab/callback"
```

启用 SSO 后的行为：
- 首次通过 GitLab 登录的用户自动创建（默认 `viewer` 角色）
- 仅读取 `username`（以及可选的 `email`、`avatar_url`），不同步 GitLab 权限
- 使用自签名证书的 GitLab 实例无需额外配置（已内置 `InsecureSkipVerify`）
- 未配置 SSO 时，登录页仅显示内置账号表单，使用 `admin` 账号即可登录

## 发版流程

```
Phase 0: 创建计划
  └─ 选择竖井 → 生成 GA 模块（internal + pending-external）→ 写入计划

Phase 1: 打 Tag（TAGGING）
  └─ 按仓库分组，模拟 tag 操作（300-800ms/仓库）

Phase 2: 依赖分析（ANALYZING）
  └─ 生成 GA→GA 依赖边，拓扑排序，环检测

Phase 2.5: 外部依赖确认（GATE_PENDING_EXTERNAL）
  └─ 等待 pending-external 节点人工确认已在 akasha 中就绪

Phase 3: 并发池发布（RELEASING）
  └─ 按拓扑序逐模块发布，上游 SUCCESS 才启动下游
  └─ 模拟：拉依赖清单 → 构建 → 回写 akasha

Phase 4: 完成（COMPLETED / ABORTED）
  └─ 记录发布历史，SSE 推送最终状态
```

## 项目结构

```
main.go                         # 入口：路由注册、存储选择（MySQL/内存）、依赖注入
internal/
├── model/
│   ├── model.go                # 领域模型：Module(GA), DepEdge(CrossRepo), ReleasePlan, PlanModuleEntry 等
│   ├── gorm.go                 # GORM 模型（gps_* 前缀表），AutoMigrate
│   ├── sort.go                 # 拓扑排序 + 环检测（Kahn 算法 + DFS 环路径定位）
│   └── user.go                 # 用户/角色/RBAC 模型
├── store/
│   └── store.go                # Store 接口定义（handler/simulator 依赖此接口）
├── mock/
│   ├── store.go                # 内存 Store 实现
│   ├── generator.go            # 模块生成（GA 格式 + pending-external + 跨 repo 边）
│   └── simulator.go            # 发版模拟器（5 阶段状态机 + akasha 写回模拟）
├── mysql/
│   └── store.go                # MySQL Store 实现（GORM）
├── sse/
│   └── broker.go               # SSE 发布/订阅（独立于存储层）
├── auth/
│   └── service.go              # GitLab OAuth2 + HS256 JWT
├── dalaran/
│   └── client.go               # dalaran API 客户端（获取产品树）
├── middleware/
│   └── auth.go                 # JWT 认证中间件
└── handler/
    ├── silo.go                 # 产品树 API（竖井、仓库）
    ├── repo.go                 # 仓库列表 + 发版分支配置
    ├── plan.go                 # 计划 CRUD + 确认 + 外部依赖确认
    ├── release.go              # 执行/中止/重试 + SSE 事件流 + 进度查询
    ├── history.go              # 发版历史
    ├── auth.go                 # 登录/登出/当前用户
    ├── admin.go                # 用户/角色管理
    └── rbac.go                 # RBAC 辅助函数
static/                         # 前端 SPA（Vanilla JS，hash 路由）
docker-compose.yml              # MySQL 8.0 容器
design.md                       # 架构设计文档（中文，权威来源）
docs/
├── design-module-identity.md   # GA 模块标识方案设计
└── das_design.md               # DAS 依赖分析系统详细设计
```

## 数据库表

| 表 | 说明 |
|---|---|
| `gps_silos` | 竖井（全局，来自 dalaran） |
| `gps_repos` | 仓库（全局，来自 dalaran） |
| `gps_release_plans` | 发版计划主表 |
| `gps_plan_modules` | 计划内模块（GA 主键，含 Kind 分类） |
| `gps_plan_dep_edges` | 计划内依赖边（含 CrossRepo 标记） |
| `gps_plan_topo_orders` | 拓扑排序结果 |
| `gps_plan_gradle_subprojects` | gradlePath→GA 映射（审计用） |
| `gps_release_histories` | 发版历史快照 |
| `gps_users` | 用户 |
| `gps_roles` | 角色 |
| `gps_user_roles` | 用户-角色关联 |

表名统一使用 `gps_` 前缀，通过 GORM AutoMigrate 自动创建。

## API 概览

所有 `/api/*` 路由需要 JWT 认证。写操作额外检查 RBAC 权限和竖井范围。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/auth/mock-login` | 内置账号登录 |
| GET | `/auth/gitlab/callback` | GitLab SSO 回调 |
| GET | `/api/silos` | 竖井列表 |
| GET | `/api/silos/:id/repos` | 竖井下的仓库 |
| GET | `/api/repos` | 全部仓库（含 can_edit） |
| PUT | `/api/repos/:id/branch` | 修改发版分支 |
| POST | `/api/plans` | 创建发版计划 |
| GET | `/api/plans` | 计划列表 |
| GET | `/api/plans/:id` | 计划详情（含模块、依赖图） |
| PUT | `/api/plans/:id/versions` | 覆盖目标版本 |
| POST | `/api/plans/:id/confirm` | 确认计划（生成依赖图 + 环检测） |
| POST | `/api/plans/:id/confirm-external` | 确认 pending-external 模块就绪 |
| POST | `/api/plans/:id/execute` | 执行发版 |
| GET | `/api/plans/:id/progress` | 发版进度 |
| GET | `/api/plans/:id/events` | SSE 实时事件流 |
| POST | `/api/plans/:id/abort` | 中止发版 |
| POST | `/api/plans/:id/modules/:mid/retry` | 重试失败模块 |
| GET | `/api/history` | 发版历史 |
| GET | `/api/history/:id` | 历史详情 |

## 技术栈

- **后端**：Go + Gin + GORM
- **前端**：Vanilla JS SPA（hash 路由，无框架依赖）
- **数据库**：MySQL 8.0（可选）
- **认证**：GitLab OAuth2 + HS256 JWT（可选，支持 mock 登录）
- **实时推送**：Server-Sent Events
- **前端构建**：`//go:embed` 嵌入，无需单独构建步骤
