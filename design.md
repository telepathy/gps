# GPS — 全局版本发布系统 设计文档

## 1. 概述

全局版本发布系统 (GPS, Global Publishing System) 用于在**Java 多项目多模块复杂依赖**背景下，完成全系统版本自动发布。每个项目使用 Gradle 组织为多个模块，不同项目/模块之间存在复杂的依赖关系。GPS 负责协调各外部系统，按拓扑顺序逐模块完成版本发布。

---

## 2. 系统架构总览

```
                          ┌─────────────────┐
                          │   产品树信息系统   │
                          │    (PTIS)       │
                          └────────┬────────┘
                                   │ 代码库位置、发布分支
                                   │ 产品/模块层级关系
                                   ▼
┌──────────────┐     ┌─────────────────────┐     ┌──────────────┐
│  依赖分析系统  │────▶│        GPS          │────▶│  发版流水线    │
│    (DAS)     │     │  (Global Publishing │     │ (CI/CD)      │
│              │◀────│       System)       │◀────│              │
└──────────────┘     └──────────┬──────────┘     └──────────────┘
                                │
                                │ 发布后更新版本信息
                                ▼
                       ┌──────────────┐
                       │  依赖管理系统   │
                       │    (DMS)      │
                       └──────────────┘
```

### 外部系统说明

| 系统 | 缩写 | 职责 | GPS 调用方向 |
|------|------|------|-------------|
| 产品树信息系统 (Product Tree Info System) | PTIS | 维护产品层级、代码库位置、发布分支 | GPS → PTIS (读) |
| 依赖分析系统 (Dependency Analysis System) | DAS | 分析模块间依赖关系，返回依赖图 | GPS → DAS (读) |
| 发版流水线 (Release Pipeline) | CI/CD | 执行单个模块的构建、测试、发布 | GPS → CI/CD (触发) |
| 依赖管理系统 (Dependency Management System) | DMS | 管理模块制品的版本坐标 (如 Maven 仓库) | GPS → DMS (写) |

---

## 3. 核心概念定义

### 3.1 产品树 (Product Tree)

产品树维护三层结构：**竖井 → 代码仓库 → 模块**。

- **竖井 (Silo)**: 顶层组织单元，一组经常一起发布的代码仓库集合。GPS 以竖井为粒度获取上一个发布版本
- **代码仓库 (Repository/Repo)**: 一个 Git 仓库，使用 Gradle 组织，内含一个或多个模块。仓库拥有独立的发布分支
- **模块 (Module)**: 最小的可发布单元，对应一个 Gradle 子项目 (`settings.gradle` 中的 `include`)。模块间存在跨仓库的依赖关系

```
Root
├── Silo-A (竖井)
│   ├── Repo-A1 (代码仓库)          ← 分支: release/2025Q2
│   │   ├── Module-A1a (模块)
│   │   └── Module-A1b (模块)
│   └── Repo-A2 (代码仓库)          ← 分支: main
│       └── Module-A2a (模块)
├── Silo-B (竖井)
│   ├── Repo-B1 (代码仓库)          ← 分支: release/2025Q2
│   │   ├── Module-B1a (模块)
│   │   └── Module-B1b (模块)
│   └── Repo-B2-Shared (代码仓库)   ← 分支: main
│       └── Module-B2-shared (模块) ← 依赖 Module-A2a
└── Silo-C (竖井)
    └── Repo-C1 (代码仓库)
        └── Module-C1a (模块)
```
> **说明**: 同一仓库的多个模块共享同一个 Git 仓库和发布分支，打 Tag 以仓库为粒度；但模块间的依赖关系以模块为粒度，发布流水线也以模块为粒度触发。

### 3.2 依赖图 (Dependency Graph)

有向无环图 (DAG)，节点为模块，边为"B 依赖 A"（即 B 的构建/发布必须在 A 之后）。GPS 对依赖图进行拓扑排序得到全序发布序列。

> 模块标识、节点分类、跨仓库依赖与版本传播的完整设计见 `docs/design-module-identity.md`，要点如下。

**模块规范主键 = GA（`group:artifact`）**。一个 Gradle 子项目有两个身份：仅在单个 build 内唯一的 `gradlePath`（`:core:api`，用于项目内 `project(...)` 依赖），与全局唯一的 Maven 坐标 GAV（用于项目间 `g:a:v` 依赖）。二者经每个仓库的 `gradlePath→GA` 解析表归一化到同一 GA 键空间，于是项目内/项目间依赖统一为 `GA → GA` 边。版本不进 ID，由 GPS 管理。

**节点恒为模块、不为 repo**：repo 级可能存在循环依赖（A 的模块依赖 B、B 的另一模块又依赖 A），但模块级保证无环。因此拓扑排序、并发池调度、上游就绪判断都在模块粒度进行；repo 仅作为打 tag / 版本归属的粒度，不是构建或拓扑单元。

**节点三分类**：
- **internal**：属于启用 devops 的 repo 的模块，本次发布产出，构成可发布节点。
- **pending-external**：自研但所在 repo 未纳入 devops、却被 internal 模块依赖的模块；不由 GPS 发布，需人工确认其已以正确版本存在于 akasha。
- **third-party**：第三方公共库（spring/guava 等），在 akasha 直接可用、无需更新，**不进 DAG、不展示**。

**跨仓库 vs 仓库内依赖**：仓库内依赖（`project(...)`）由 Gradle 在单个 build 内自行解析，不经 DMS；只有跨仓库依赖（二方包）通过 DMS（akasha）记录与传播版本。因此 DMS 中登记的模块是全部模块的子集。

### 3.3 版本号规则

- GPS 可自动从 PTIS 获取每个仓库的上一个发布版本，并自动递增 (如 `1.2.3` → `1.2.4`)
- 版本号也可在集中发版前由人工修改/确认
- 最终版本号以 GPS 中的确认版本为准
- **版本是仓库级的**：一次发布对所有启用 devops 的仓库在发布分支上统一打一组 tag（一组 tag = 一个版本），冻结本次发布的源码快照；模块继承其所属仓库本次 tag 的版本。后续构建/发布均以这组 tag 为准，过程中唯一动态变化的是经 DMS 传播的跨仓库依赖版本号。

---

## 4. 系统流程

### 4.1 整体发布流程

```
Phase 0: 发版准备 (Pre-Release Configuration)
  ├── 0a. 从 PTIS 获取产品树信息（代码库位置、发布分支）
  ├── 0b. 确定每个仓库拟发布版本号（自动递增 + 人工确认）
  └── 0c. 指定本次发布使用的 akasha (DMS) 依赖分支

Phase 1: 打 Tag — 统一冻结源码快照 (Tagging)
  └── 对全部启用 devops 的代码库，在发布分支上统一打一组新版本 tag，冻结本次源码；
       后续分析与发布均以这组 tag 为准

Phase 2: 依赖分析与拓扑排序 (Dependency Resolution & Topological Sort)
  ├── 2a. 调用 DAS，基于本次 tag 的源码获取归一化的模块级 GA→GA 依赖边
  ├── 2b. 按 GA 归类节点：internal / pending-external / third-party
  ├── 2c. 拓扑排序，生成全序发布序列
  └── 2d. 模块级环检测：若存在环则中止发布并定位环路径

Phase 2.5: 外部依赖确认 (Pending-External Gate)
  └── 列出 pending-external 节点（自研但未 devops 的依赖），人工确认其已以正确版本
       存在于指定的 akasha 分支；确认后置为"已满足上游"，放行其下游

Phase 3: 并发池发布 (Concurrent Pool Release)
  └── 维护固定大小的并发池，按拓扑序逐个取出模块，若其上游依赖已全部完成则投入池中执行，
       池满时阻塞等待。所有模块处理完毕后结束。
       └── 单模块发布：从 akasha 拉依赖清单注入构建 → checkout tag 构建/测试/发布
            → published 模块回写 akasha 新版本 → 解锁下游

Phase 4: 发布完成 (Post-Release)
  ├── 记录发布日志
  └── 通知相关方
```

### 4.2 详细流程

#### Phase 0: 发版准备

1. 调用 PTIS API 获取指定竖井下所有仓库和模块的信息（repo_url、release_branch 等）
2. 展开得到本次涉及的全部模块列表
3. 对每个模块调用 PTIS 获取上一发布版本，自动计算下一版本（patch 递增）；如有人工输入的 `version_overrides`，覆盖自动计算的结果
4. 生成 Release Plan，按竖井→仓库→模块组织

#### Phase 1: 打 Tag（统一冻结源码快照）

对本次涉及的**全部启用 devops 的仓库**，在各自的**发布分支**上统一打一组新 tag（版本自动 +1 或人工指定），仓库间并行：
1. clone 仓库到发布分支
2. 对该仓库打 tag（同一仓库的多个模块共享标签）
3. push tags

这组 tag 一旦打完即冻结本次发布的全部源码；后续依赖分析与发布都以这组 tag 为准（checkout 该 tag 构建），不存在"靠后模块用了更新源码"的问题。

#### Phase 2: 依赖分析与拓扑排序

1. 调用 DAS API，基于**本次这组 tag 对应的源码**分析依赖，归一化为模块级 `GA → GA` 边集合（项目内/项目间统一，第三方库丢弃）
2. 按 GA 归类节点：internal / pending-external / third-party（见 §3.2）
3. GPS 对模块级依赖图进行拓扑排序，得到全序发布序列 `sorted_order`
4. **模块级环检测：若存在环，则不允许进入后续发布**，报错并定位环上的模块链

#### Phase 2.5: 外部依赖确认（pending-external 门控）

进入并发池前，列出本次涉及的全部 pending-external 节点（自研但未 devops 的依赖），要求人工确认其已以**正确版本**存在于本次指定的 akasha 分支中。确认后这些节点被置为"已满足上游"，放行其下游 internal 模块；GPS 不对它们打 tag 或触发流水线。

#### Phase 3: 并发池发布

维持一个固定大小的并发池（`concurrency` 可配置，默认 4），按拓扑序遍历 `sorted_order`：
- 对每个模块，检查其所有上游依赖是否已发布成功
  - 若满足 且 池有空闲槽位 → 投入池中异步执行发布
  - 若池满或上游未就绪 → 阻塞等待
- 单个模块的发布流程（池中 worker 执行）：
  1. 从 DMS（akasha）按指定分支拉取依赖清单注入构建（仅解析跨仓库依赖，引用上游模块此刻最新版本）
  2. checkout 本次 tag → 触发 CI/CD 流水线 → 轮询状态
  3. SUCCESS 且该模块被跨仓库消费（published）→ 回写 DMS 登记新 GAV（供下游拉取）；FAILED 则按失败策略处理
- 所有模块处理完毕且池中无运行任务 → 结束

**失败策略**: ABORT（立即中止）/ SKIP（跳过该模块及其下游传递闭包，继续其他）/ RETRY（重试 N 次，仍失败按 ABORT/SKIP 处理）。默认 ABORT + RETRY(3)。

#### Phase 4: 发布完成

生成发布报告（成功/失败列表、耗时、版本变更摘要），通知相关方。

---

## 5. 与外部系统的接口定义

### 5.1 产品树信息系统 (PTIS)

PTIS 维护竖井→仓库→模块的层级关系、各仓库的 Git 地址与发布分支、各模块的历史发布版本。

| 接口 | 方向 | 用途 |
|------|------|------|
| 获取产品树 | GPS → PTIS | 根据竖井 ID 拉取其下所有仓库和模块（repo_url, release_branch 等） |
| 获取上一发布版本 | GPS → PTIS | 根据 silo_id + module_id 查询该模块在对应竖井下的最近发布版本号 |
| 记录新发布版本 | GPS → PTIS | 模块发布成功后回写新版本记录 |

### 5.2 依赖分析系统 (DAS)

DAS 负责分析 Java Gradle 多模块项目间的依赖关系（基于静态分析 `build.gradle` 或 `gradle dependencies` 输出）。

| 接口 | 方向 | 用途 |
|------|------|------|
| 获取依赖图 | GPS → DAS | 传入本次 tag 对应的仓库快照，返回每仓库的子项目清单（`gradle_path, group, artifact`）+ 归一化的 `GA → GA` 边集合 + 引用到的非 internal GA 集合，供 GPS 分类节点与拓扑排序 |

- 基于**本次一组 tag 的源码**分析（而非分支 HEAD），与冻结快照一致。
- 项目内依赖（`project(...)`）经仓库的 `gradlePath→GA` 表归一化，与项目间依赖统一为 GA 边；第三方库丢弃。
- GPS 拿到边集合后做**模块级环检测**，有环则中止发布。

### 5.3 发版流水线 (CI/CD)

发版流水线负责执行单个模块的构建、测试、发布流程。

| 接口 | 方向 | 用途 |
|------|------|------|
| 触发发布流水线 | GPS → CI/CD | 提交模块信息（module_id, version, repo_url, branch, tag 等），触发构建发布，返回 pipeline_id |
| 查询流水线状态 | GPS → CI/CD | 根据 pipeline_id 轮询当前状态（PENDING / RUNNING / SUCCESS / FAILED） |

### 5.4 依赖管理系统 (DMS)

DMS 由 **akasha** 项目承担（详见 `docs/design-module-identity.md` §8）。akasha 是集中式 Gradle 依赖版本登记中心，**不改写 build 文件**，而是作为跨仓库二方包版本的单一事实来源，按分支输出可 `apply from:` 的依赖清单。build 文件里依赖写成 `libraries["<artifact>"]`，版本不写死，由清单动态注入。

| 接口 | 方向 | 用途 |
|------|------|------|
| 拉取分支依赖清单 | GPS → DMS | 模块构建前按指定分支拉取最新依赖清单，注入构建（引用上游模块此刻最新版本） |
| 注册/更新制品版本 | GPS → DMS | published 模块发布成功后，向指定分支追加新 GAV（append-only，保留历史） |
| 查询分支最新版本 | GPS → DMS | （可选）发版前查询某分支某模块的当前最新版本 |

- **join key = artifact 短名（GAV 里的 `a`）**：akasha 的依赖短名 `Name` 即 artifact；同一分支内 artifact 须唯一。
- **按分支管理依赖**：每个分支是一份独立的依赖清单快照，相互隔离。**每次发布必须先指定 akasha 分支**（对应 ReleasePlan 的 `dms_branch`，为创建计划的必填前置）。拉取、回写、pending-external 确认全部针对该分支。
- **append-only + 闪回**：更新版本不改旧记录而是插入新记录，可按时间点回溯某次发布的依赖快照。
- 版本传播机制（"先写库 → 后读库"）：上游模块发布后回写新版本 → 下游模块构建时拉到该最新版本，从而正确表达"先发布的模块影响 DAG 靠后的模块"。

---

## 6. GPS 内部数据结构

### 6.1 ReleasePlan (发布计划)

记录一次发版的全量信息：所属竖井、DMS 依赖分支、并发池大小、失败策略等。以竖井→仓库→模块三层嵌套组织，每个模块记录：前一版本号、目标版本号、是否自动递增、当前状态（PENDING / TAGGED / RELEASING / SUCCESS / FAILED）。

### 6.2 DependencyGraph (依赖图)

包含节点集合（模块 ID 列表）、边集合（from→to，语义为 to 依赖 from）、拓扑排序后的全序序列 `sorted_order` 和循环依赖检测结果。

### 6.3 ReleaseResult (发布结果)

记录发布执行结果：每个模块的最终状态、耗时、pipeline_id、错误信息，以及汇总统计（总数/成功/失败/跳过）。

---

## 7. GPS 核心模块设计

### 7.1 模块划分

```
gps/
├── gps-core/                    # 核心调度与协调
│   ├── ReleaseOrchestrator      # 发布编排器，控制整体流程
│   ├── TopologicalSorter        # 依赖图拓扑排序
│   ├── ConcurrentReleasePool    # 并发池, 基于拓扑序 + 上游就绪条件调度发布
│   └── ReleasePlanManager       # 发布计划管理
├── gps-config/                  # 配置管理
│   ├── VersionCalculator        # 版本号自动递增计算
│   └── ConfigStore              # 配置持久化
├── gps-git/                     # Git 操作
│   └── TagManager               # 批量打 Tag 操作
├── gps-integration/             # 外部系统集成
│   ├── ptis/PtisClient          # 产品树信息系统客户端
│   ├── das/DasClient            # 依赖分析系统客户端
│   ├── cicd/CiCdClient          # 发版流水线客户端
│   └── dms/DmsClient            # 依赖管理系统客户端
├── gps-persistence/             # 数据持久化
│   ├── ReleasePlanRepository
│   └── ReleaseResultRepository
└── gps-api/                     # GPS 对外 API
    ├── ReleaseController        # 发布 REST API
    └── PlanController           # 计划管理 REST API
```

### 7.2 核心逻辑

```
ReleaseOrchestrator.execute(planRequest):

  // Phase 0: 准备
  plan = releasePlanManager.create(planRequest)
    - ptisClient 拉取产品树
    - versionCalculator 计算目标版本号 (自动递增 + 人工覆盖)

  // Phase 1: 打 Tag (仓库级并行)
  for each repo in plan.repos (并行):
    clone → 对该仓库所有模块打 tag → push

  // Phase 2: 依赖分析与拓扑排序
  depGraph = dasClient.getDependencyGraph(module_ids, dms_branch)
  sortedOrder = topologicalSorter.sort(depGraph)
  检测循环依赖

  // Phase 3: 并发池发布
  pool = ConcurrentReleasePool(concurrency, isUpstreamReady, failureHandler)
  for module_id in sortedOrder:
    pool.submit(module):              // 阻塞至上游就绪 + 有空闲槽位
      触发 CI/CD 流水线 → 轮询 → SUCCESS 则更新 DMS / FAILED 则按策略处理
  pool.awaitAll()

  // Phase 4: 完成
  生成发布报告，通知相关方
```

### 7.3 失败策略

| 策略 | 行为 | 适用场景 |
|------|------|---------|
| ABORT | 已运行任务完成后不再提交新任务，剩余模块标记 SKIPPED | 严格版本一致性要求 |
| SKIP | 跳过失败模块，并自动跳过其后所有直接或间接依赖它的下游模块 | 部分模块可独立发布 |
| RETRY | 失败后重试 N 次，仍失败则按 ABORT/SKIP 处理 | 偶发性构建失败 |

建议默认策略为 ABORT + RETRY(3次)。

---

## 8. GPS 对外 API

| 接口 | 用途 |
|------|------|
| 创建发布计划 | 提交 silo_id、dms_branch、version_overrides、concurrency、失败策略等，GPS 从 PTIS 拉取产品树并计算目标版本号，返回 plan |
| 执行发布 | 对指定 plan_id 开始执行发布流程（异步），返回 RUNNING 状态 |
| 查询发布进度 | 查询指定 plan 的当前执行阶段、各模块状态和完成进度 |
| 查询发布结果 | 查询指定 plan 的最终发布结果和汇总统计 |
| 查询全量仓库 | `GET /api/repos`，返回所有仓库（含所属竖井名与当前用户的 `can_edit` 标记），仅需登录 |
| 配置仓库发布分支 | `PUT /api/repos/:id/branch`，需 `release` 动作 + 该仓库所属竖井在用户授权范围内 |

> 用户认证、当前用户、登出、用户/角色管理等接口见第 10.4 节。所有发布相关接口均需登录，写操作按角色与竖井范围鉴权。

---

## 9. 关键设计决策与注意事项

### 9.1 为什么打 Tag 与执行流水线分离，且以仓库为粒度？

- **仓库粒度打 Tag**: 同一仓库的多个模块共享 Git 仓库和发布分支，一次 clone、批量打 tag、一次 push，避免重复操作。仓库间 Tag 操作完全独立，可并行执行
- **并行效率**: Tag 是纯 Git 操作，不依赖模块间关系，可完全并行
- **失败隔离**: Tag 失败不影响已成功的 Tag，也不阻塞后续流程
- **审计需求**: Tag 时间戳独立于流水线执行时间，便于追溯

### 9.2 为什么用拓扑排序 + 并发池？

- **避免"层"的刚性约束**: 按层级分组意味着即使上层只剩一个慢任务，同层其他已完成的下游也不能提前开始。并发池模型下，只要某个模块的上游全部就绪且有空闲槽位，即可立即投入，更充分利用并发能力
- **实现更简单**: 不需要额外的分层计算逻辑，拓扑排序一次即可。池的调度只需判断"上游是否全完成"一个条件
- **池大小可控**: `concurrency` 参数直接控制并发度，资源敏感的发布环境可调小，追求速度时可调大
- **依赖一致性**: 上游就绪检查确保下游发布时使用的都是上游的最终发布版本

### 9.3 版本号自动递增规则

GPS 默认使用语义化版本 (SemVer)，自动递增规则：
- `MAJOR.MINOR.PATCH` → 递增 PATCH: `1.2.3` → `1.2.4`
- 若人工指定了部分版本号，则不自动递增
- 首次发布的模块默认版本为 `0.1.0`

### 9.4 并发与一致性

| 阶段 | 并发模型 | 说明 |
|------|---------|------|
| 打 Tag | 仓库级并行 | 同一仓库内模块串行打 tag，不同仓库间并行 |
| 依赖分析 | 单次调用 | DAS 返回完整图 |
| 拓扑排序 | 纯计算 | 本地单次排序，得到全序序列 |
| 流水线发布 | 并发池 (拓扑序 + 上游就绪) | 固定大小线程池，按拓扑序消费，模块上游全部就绪 + 有空闲槽位时投入。concurrency 可配置 |

### 9.5 安全与鉴权

GPS 与各外部系统通信时需携带认证信息 (API Token / SSH Key)，建议通过环境变量或密钥管理服务 (Vault) 注入。

GPS 自身的用户认证与权限模型见第 10 章。

---

## 10. 用户系统与 RBAC

GPS 的用户身份来自一个**自签名 GitLab 实例的 SSO**，GPS 只读取 GitLab 用户名作为唯一身份标识；权限控制（RBAC）完全由 GPS 内部维护，与 GitLab 的组织/项目权限解耦。

### 10.1 认证流程 (GitLab OAuth2)

```
浏览器 → GET /auth/login                 # 登录页：GitLab 按钮 + 内置账号表单
       → GET {gitlab}/oauth/authorize    # 跳转 GitLab 授权 (scope=read_user)
GitLab → GET /auth/gitlab/callback?code  # 授权回调
GPS    → POST {gitlab}/oauth/token       # code 换 access_token
       → GET  {gitlab}/api/v4/user       # 取用户信息，仅消费 username
       → 签发 JWT，写入 HttpOnly Cookie，302 回首页
```

- **自签名证书**：GitLab 为内网自签名实例，GPS 的 OAuth HTTP 客户端 (resty) 配置 `InsecureSkipVerify: true` 跳过 TLS 证书校验。
- **只读用户名**：GPS 仅从 `/api/v4/user` 取 `username`（email/avatar 仅用于展示）；首次登录的 GitLab 用户自动建档并赋予 `viewer` 角色，再次登录保留既有角色与竖井范围。
- **会话**：HS256 JWT（claims: `user_id`/`username`/`exp` 24h），写入 HttpOnly Cookie。请求时从 `Authorization: Bearer` 或 Cookie 读取。同源 SSE 自动携带 Cookie。
- **冷启动 / Mock 登录**：系统内置一个 `admin` 账号（`allowed_silos="*"`）。`POST /auth/mock-login` 支持以用户名直接登录已存在用户，无需 GitLab，用于初始化与无 SSO 环境的调试。未配置 GitLab 时登录页仅展示内置账号表单。

### 10.2 角色与权限

GPS 采用「角色 → 动作」+「用户 → 竖井范围」两级模型。

**动作 (Action)**

| 动作 | 含义 |
|------|------|
| `view` | 查看（计划列表/详情、进度、历史、SSE） |
| `create` | 创建发版计划 |
| `release` | 确认计划、修改版本、执行/中止/重试发版 |
| `manage` | 用户与角色管理 |

**内置角色 (Role)**

| 角色 | 动作 | 说明 |
|------|------|------|
| `admin` | view + create + release + manage | 管理员，跳过竖井范围限制 |
| `releaser` | view + create + release | 发布者，仅在授权竖井范围内可写 |
| `viewer` | view | 观察者，只读（新用户默认） |

**竖井范围 (allowed_silos)**

用户级字段，控制可操作的竖井：`"*"` = 全部，`""` = 无，或逗号分隔的 silo_id 列表（如 `silo-001,silo-002`）。`admin` 与 `allowed_silos="*"` 跳过该校验。

### 10.3 鉴权点

- 所有 `/api/*` 接口经 `RequireAuth` 中间件，未携带有效 JWT 返回 401（前端据此跳转登录页）。
- 写操作叠加校验：
  - 创建计划 → 需 `create` 动作 + 目标竖井在用户授权范围内。
  - 确认 / 改版本 / 执行 / 中止 / 重试 → 需 `release` 动作 + 计划涉及的全部竖井在授权范围内。
- 用户管理接口 (`/api/admin/*`) → 需 `manage` 动作。
- 读操作仅需登录。

### 10.4 用户系统 API

| Method | Path | 鉴权 | 用途 |
|--------|------|------|------|
| GET | /auth/login | 公开 | 登录页 |
| POST | /auth/mock-login | 公开 | 内置账号 / 用户名登录 |
| GET | /auth/gitlab/callback | 公开 | GitLab OAuth 回调 |
| GET | /api/current-user | 登录 | 当前用户（含角色） |
| POST | /api/logout | 登录 | 登出（清 Cookie） |
| GET | /api/admin/users | manage | 用户列表 |
| GET | /api/admin/roles | manage | 角色列表 |
| POST | /api/admin/users/import | manage | 批量预注册用户 |
| PUT | /api/admin/users/:uid/roles | manage | 设置用户角色 |
| PUT | /api/admin/users/:uid/access | manage | 设置用户竖井范围 |

### 10.5 用户导入 (预注册 + SSO 绑定)

身份始终由 GitLab SSO 管理，GPS 不存储任何密码。管理员可**批量预注册用户名**，用于在用户首次登录前预先分配角色与竖井范围。

```
POST /api/admin/users/import
{
  "users": [
    { "username": "zhangsan" },
    { "username": "lisi", "roles": ["releaser"], "allowed_silos": "silo-001,silo-002" },
    { "username": "wangwu", "roles": ["viewer"] }
  ]
}
→ { "created": [...], "skipped": [...], "failed": { "username": "reason" } }
```

- **预注册状态**：导入的用户 `GitlabID=0`，仅占位用户名与权限，尚未真正登录。
- **SSO 绑定**：该用户首次通过 GitLab 登录时，`FindOrCreateUser` 按 username 匹配到预注册记录，绑定 GitLab 信息（id / email / avatar），并**保留导入时设置的角色与竖井范围**；未预注册的用户按新用户处理，赋予 `viewer`。
- **幂等**：用户名已存在则跳过（不覆盖既有角色）；`roles` 为空默认 `viewer`；未知角色名拒绝并计入 `failed`。

### 10.6 配置 (环境变量)

| 变量 | 说明 |
|------|------|
| `GPS_JWT_SECRET` | JWT 签名密钥；未设置则生成临时密钥（重启后会话失效） |
| `GPS_GITLAB_URL` | GitLab 实例地址，如 `https://gitlab.internal.com` |
| `GPS_GITLAB_APP_ID` | OAuth 应用 ID；设置后启用 GitLab SSO |
| `GPS_GITLAB_APP_SECRET` | OAuth 应用密钥 |
| `GPS_GITLAB_CALLBACK_URL` | 回调地址，指向 `/auth/gitlab/callback` |
| `GPS_DALARAN_URL` | dalaran 地址（**必填**）；从其 `GET /api/v1/silos` 加载竖井/仓库；未配置或拉取失败则启动失败退出 |

> 当前原型用户/角色数据存于内存（重启丢失），与第 6 章「内部数据结构」一致；未来可平移至关系库（`user` / `role` / `user_roles` 三表）。

---

## 11. UI 设计

GPS Web 控制台提供发版全流程的可视化操作与监控。

### 11.1 发版计划创建页
选择竖井，填写 DMS 依赖分支、并发池大小、失败策略。系统自动拉取该竖井下的仓库与模块列表，展示每个模块的上一版本和自动计算的下一版本，支持逐模块人工覆盖版本号。确认后生成发布计划。

### 11.2 版本确认页
以表格展示本次发版涉及的全部模块（按仓库分组），列出当前版本 → 目标版本。人工修改的版本高亮标记。支持一键全部确认或逐模块确认。确认后进入执行阶段。

### 11.3 发版监控页
核心页面。左侧为依赖关系 DAG 图，节点颜色表示模块状态（待发布/发布中/成功/失败/跳过）。右侧为实时日志流和执行进度条。点击节点可查看对应的 CI/CD 流水线链接和详细日志。

### 11.4 发布历史页
列表展示历史发布记录（时间、竖井、涉及模块数、成功/失败统计）。点击进入详情页，查看完整的发布结果、各模块耗时、失败原因。支持导出发布报告。

### 11.5 仓库管理页
扁平表格列出全量仓库（仅展示 dalaran 中 `devopsOpt=true` 的仓库），四列：竖井代码、仓库名、发布分支、操作。任意登录用户可查看，顶部提供竖井代码过滤框做即时筛选。仓库名为链接，点击在新窗口打开其网页地址（由 SSH 地址转换：去 `ssh://`/`git@` 前缀与端口、去 `.git` 后缀、改为 `https://`）。对所属竖井在用户授权范围内的仓库，可就地编辑并保存其发布分支（`release_branch`）；无权限的仓库分支只读并标注「无权限」。编辑权限由后端 `can_edit` 标记驱动，写入时再做一次竖井范围硬校验。

### 11.6 权限管理页
仅 `admin` 可见。顶部提供「导入用户」区，可批量粘贴用户名（每行一个，支持 `用户名,角色,竖井` 格式）预注册用户——身份仍由 GitLab SSO 管理，用户首次登录时自动绑定并保留此处设置的角色。下方表格列出全部用户，支持勾选角色、编辑竖井访问范围（`allowed_silos`）并保存。导航栏右侧常驻用户区（头像/用户名/角色/登出）。未登录访问任何页面自动跳转登录页。

---

## 12. 扩展考虑 (未来规划)

1. **增量发布**: 仅发布有变更的模块及其下游依赖链，而非全量
2. **断点续发**: 发布中途中断后，重启时自动跳过已完成模块，从断点继续
3. **预检机制**: Phase 2 后增加一次全面预检 (UT/集成测试)，减少流水线失败率
4. **回滚机制**: 发布失败后自动回滚 DMS 中已注册的制品版本
5. **变更日志自动生成**: 基于 Git commit 历史自动生成 Release Notes
6. **审批流程集成**: 发布计划需经审批后方可执行
