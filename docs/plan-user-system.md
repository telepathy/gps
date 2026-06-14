# GPS 用户系统 + GitLab SSO + RBAC 实施计划

> 参考 `telepathy/aaru` 的认证/RBAC 模式，适配 GPS 当前**全内存原型**架构。
>
> **状态：已实现。** 最终落地与本计划的差异：① OAuth HTTP 客户端采用 **resty**（`SetTLSClientConfig(InsecureSkipVerify:true)`），非标准库；② JWT 使用 **golang-jwt/jwt/v5**；③ 冷启动改为 **内置 `admin` 账号 + mock 登录**（`POST /auth/mock-login`），未实现 `POST /api/init`。规范说明以 `design.md` 第 10 章与 `CLAUDE.md` 为准。

## 决策（已确认）

| 维度 | 选择 |
|------|------|
| 存储 | 内存 store（沿用 `mock.Store` + `sync.RWMutex`，重启丢失） |
| RBAC 粒度 | 角色 + 竖井范围（`allowed_silos`） |
| 鉴权范围 | 所有 `/api/*` 需登录；写操作再按角色/竖井校验 |
| 自签名 GitLab | OAuth HTTP 客户端 `InsecureSkipVerify: true` |
| 会话 | JWT 写入 HttpOnly cookie（24h），同源 SSE 自动携带 |

## 角色模型

三个内置角色（启动时种子化）：

| 角色 | 权限 |
|------|------|
| `admin` | 全部：管理用户/角色、创建计划、执行发版、查看；`allowed_silos="*"` |
| `releaser` | 在授权竖井范围内创建计划 + 执行/中止/重试发版 + 查看 |
| `viewer` | 只读（列表、详情、进度、历史、SSE） |

权限动作（action）：`manage`（用户管理）、`create`（建计划）、`release`（执行发版）、`view`（查看）。
竖井范围：用户 `AllowedSilos` 字段，`"*"` = 全部，或逗号分隔的 silo_id 列表。admin 跳过竖井校验。

## 一、后端改动

### 1. 新增数据模型 `internal/model/user.go`
```go
type User struct {
    ID           int       `json:"id"`
    Username     string    `json:"username"`      // 来自 GitLab，唯一
    Email        string    `json:"email"`
    AvatarURL    string    `json:"avatar_url"`
    GitlabID     int64     `json:"gitlab_id"`
    Roles        []string  `json:"roles"`         // 角色名列表，如 ["releaser"]
    AllowedSilos string    `json:"allowed_silos"` // "" / "*" / "silo-001,silo-002"
    CreatedAt    time.Time `json:"created_at"`
}

type Role struct {
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Actions     []string `json:"actions"` // manage/create/release/view
}
```
新增请求体：`SetUserRolesRequest{Roles []string}`、`UpdateUserAccessRequest{AllowedSilos string}`、`InitSystemRequest{Username string}`。

### 2. Store 扩展 `internal/mock/store.go`
- `Store` 增加字段：`Users map[string]*model.User`（key=username）、`Roles map[string]*model.Role`、`userCounter int`。
- 启动时 `seedRoles()` 注入 admin/releaser/viewer。
- 方法：`FindOrCreateUser(u) (*User,isNew,err)`、`GetUserByUsername`、`GetUserByID`、`ListUsers`、`SetUserRoles(id,roles)`、`SetUserAllowedSilos(id,silos)`、`GetRoles`、`CountUsers`。
- 新用户默认分配 `viewer`。
- 全部走现有 `mu sync.RWMutex`。

### 3. 鉴权服务 `internal/auth/service.go`（新建包）
- `Service{ jwtSecret []byte; gitlabURL, appID, secret, callbackURL string }`
- `ConfigureGitlab(...)`、`IsGitlabConfigured()`、`GitlabAuthURL()` → `{url}/oauth/authorize?client_id=..&redirect_uri=..&response_type=code&scope=read_user`
- `ExchangeCode(code) (*GitlabUser, error)`：
  - 用 **`net/http.Client` + `&http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}`** 跳过自签名校验（不引入 resty，用标准库）。
  - POST `{url}/oauth/token`（form：client_id/secret/code/grant_type=authorization_code/redirect_uri）→ 拿 access_token。
  - GET `{url}/api/v4/user` 带 `Authorization: Bearer`，解析 `{id, username, email, avatar_url}`，**只取 username**（空则报错）。
- `GenerateToken(user)` / `ParseToken(str) (userID int, username string, err)`：HS256，claims = user_id/username/exp(24h)/iat。
- **JWT 依赖**：优先 `github.com/golang-jwt/jwt/v5`（`go get`）。若离线无法拉取，回退为标准库 `crypto/hmac`+`encoding/base64` 自实现 ~30 行签名 token（计划默认用 golang-jwt，构建失败再回退）。

### 4. 中间件 `internal/middleware/auth.go`（新建包）
- `RequireAuth()`：从 `Authorization: Bearer` 或 cookie `token` 取 JWT → `ParseToken` → `c.Set("user_id"/"username")`；失败返回 401 JSON。
- 辅助函数（在 handler 包）：
  - `currentUser(c) *User`
  - `requireAction(c, store, action) bool`：查用户角色聚合 actions，缺失则 403。
  - `canReleaseSilos(user, siloIDs) bool`：admin 或 `AllowedSilos` 覆盖目标计划所有 silo。

### 5. 认证 handler `internal/handler/auth.go`（新建）
- `GET  /auth/login` → 返回登录页（GitLab 按钮）。GitLab 未配置时展示提示。
- `GET  /auth/gitlab/callback` → `ExchangeCode` → `FindOrCreateUser` → `GenerateToken` → `SetCookie("token", t, 86400, "/", "", false, true)` → 302 到 `/`。
- `POST /api/logout` → 清 cookie → 302 `/auth/login`。
- `GET  /api/current-user` → 返回当前 user（含 roles）。

### 6. 用户管理 handler `internal/handler/admin.go`（新建，全部 `requireAction("manage")`）
- `POST /api/init`：仅当 `CountUsers()==0` 可用，创建首个 admin（`AllowedSilos="*"`），返回 token。**冷启动引导**。
- `GET  /api/admin/users`：列出用户。
- `GET  /api/admin/roles`：列出角色。
- `PUT  /api/admin/users/:uid/roles`：设角色。
- `PUT  /api/admin/users/:uid/access`：设 `allowed_silos`。

### 7. 路由与写操作校验 `main.go`
- 读取配置（env）：`GPS_JWT_SECRET`、`GPS_GITLAB_URL`、`GPS_GITLAB_APP_ID`、`GPS_GITLAB_APP_SECRET`、`GPS_GITLAB_CALLBACK_URL`。无 secret 时生成随机内存 secret（重启失效，原型可接受）。
- `/auth/*` 公开；`POST /api/init` 公开（仅无用户时有效）。
- `api := r.Group("/api"); api.Use(authMiddleware.RequireAuth())` —— 所有现有接口纳入。
- 在写 handler 内追加角色/竖井校验：
  - `CreatePlan` → `requireAction("create")`；创建后校验 `canReleaseSilos(user, req.SiloIDs)`。
  - `ConfirmPlan/Execute/Abort/RetryModule/UpdateVersions` → `requireAction("release")` + 竖井校验（按 plan.SiloIDs）。
  - 读接口（silos/plans/progress/events/history）仅需登录。
- handler 构造函数注入 `store`（已有）；新增 handler 注入 `store` + `auth.Service`。

## 二、前端改动

### 1. `static/js/api.js`
- `fetch` 增加 `credentials: 'same-origin'`；遇 `401` → `window.location.href='/auth/login'`。
- 新增：`getCurrentUser()`、`logout()`、`getUsers()`、`getRoles()`、`setUserRoles()`、`setUserAccess()`、`initSystem()`。

### 2. `static/index.html`
- 导航栏右侧加用户区：用户名 + 头像首字母 + 角色 + 退出按钮。
- 仅 admin 显示「权限管理」导航项（`id=nav-admin`，默认隐藏）。
- 引入新页面脚本 `js/pages/admin.js`。

### 3. `static/js/app.js`
- `init()` 先 `await checkAuth()`（GET `/api/current-user`，失败跳登录页，存全局 `currentUser`），再路由。
- 新增路由 `#/admin` → `AdminPage`，进入前校验 `currentUser.roles.includes('admin')`，否则提示并回首页。
- 根据角色隐藏「创建发版」入口给 viewer（前端软隐藏，后端硬校验）。

### 4. 新增页面 `static/js/pages/admin.js`
- 用户列表表格：用户名/角色（多选下拉）/allowed_silos（输入）/保存。
- 调 `PUT /api/admin/users/:uid/roles`、`/access`。

### 5. 登录页
- 服务端 `GET /auth/login` 直接返回一段内联 HTML（含 GitLab 登录按钮跳 `GitlabAuthURL()`），不依赖 SPA，避免未登录时加载受保护资源。

## 三、验证

1. `go build -o gps-server .` 通过。
2. 未配置 GitLab：`/auth/login` 显示「未配置」提示；`POST /api/init` 可建首个 admin 并拿 token。
3. 带 token cookie 访问 `/api/current-user` 返回用户；无 token 访问 `/api/silos` 返回 401。
4. viewer 调 `POST /api/plans` 返回 403；releaser 在授权竖井内可建、非授权竖井 403；admin 全通。
5. SSE `/api/plans/:id/events` 在已登录浏览器正常（cookie 自动携带）。

## 四、文件清单

**新增**
- `internal/model/user.go`
- `internal/auth/service.go`
- `internal/middleware/auth.go`
- `internal/handler/auth.go`
- `internal/handler/admin.go`
- `static/js/pages/admin.js`

**修改**
- `internal/mock/store.go`（用户/角色存储 + 种子）
- `internal/handler/plan.go`、`release.go`（写操作校验）
- `main.go`（配置、路由分组、鉴权挂载、登录页）
- `static/index.html`、`static/js/api.js`、`static/js/app.js`

**依赖**
- `github.com/golang-jwt/jwt/v5`（`go get`；离线则回退标准库 HMAC 自实现）

## 备注
- 与 CLAUDE.md 的 DDL 未来落库目标兼容：`User/Role` 内存结构字段对齐 aaru 风格，后续可平移到关系库。
- 暂不实现 GitLab refresh token、登出黑名单（原型阶段 24h 过期即可）。
