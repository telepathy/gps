# DAS 详细设计:依赖分析系统(Dependency Analysis System)

> 本文是 GPS 设计文档(`design.md` §5.2)中 DAS 的详细设计,以及 `docs/design-module-identity.md` 中"模块标识与依赖归一化"的落地方案。
>
> **职责**:给定一组仓库及其本次发布的 tag,分析出**模块级依赖图**(节点为 GA、边为 `GA → GA`),供 GPS 做拓扑排序、环检测与并发池发布编排。

## 1. 输入与输出

### 1.1 输入

```json
{
  "plan_id": "plan-001",
  "akasha_branch": "202603",
  "repos": [
    { "repo_id": "repo-0012", "repo_url": "ssh://git@codeup.../issuance.git", "tag": "v2026.03" },
    { "repo_id": "repo-0007", "repo_url": "ssh://git@codeup.../settle.git",   "tag": "v2026.03" }
  ]
}
```

- 一组仓库 + **各自本次发布的 tag**(已由 GPS Phase 1 统一打好,源码已冻结)。
- **akasha 分支**:仓库通过 `apply from: akasha/dependency?branch=` 注入跨仓库依赖版本,分析时必须指定与本次发布一致的分支。

### 1.2 输出(GPS 归一化后的目标形态)

模块级有向图(节点 = GA,版本剥离):

```
节点(GA):
  com.csdc.spot:issuance-core-api      internal
  com.csdc.spot:issuance-core-model    internal
  com.csdc.settle:settle-client        internal (来自另一仓库)
  com.csdc.legacy:foo                  pending-external (自研未-devops)
  # org.springframework:spring-core    third-party,丢弃,不进图

边(GA → GA,from 被依赖、to 依赖方):
  issuance-core-model → issuance-core-api   (repo 内边, cross_repo=false)
  settle-client       → issuance-core-api   (跨 repo 边, cross_repo=true)
  foo                 → issuance-core-model  (跨 repo 边, cross_repo=true)
```

对应 GPS 内部 `model.DependencyGraph{Nodes, Edges, SortedOrder}`,落库见 §6。

## 2. 核心原则:驱动 Gradle 自报模型,不静态解析

**不解析 build.gradle 文本**。Gradle 构建脚本是图灵完备的 Groovy/Kotlin 代码,存在条件依赖、`apply from:` 远程脚本(akasha)、version catalog、`subprojects {}` 批量配置等,静态文本解析必然失真。

**唯一权威做法:让 Gradle 自己评估完项目后导出模型。** 通过 `--init-script` 注入一段只读脚本,在 `projectsEvaluated` 阶段遍历所有子项目,导出:
- 每个子项目的坐标(`gradle_path` + `group` + `artifact`);
- 每个子项目**声明的**(declared,非解析传递闭包)依赖,区分 `ProjectDependency`(项目内)与 `ExternalModuleDependency`(项目间/三方)。

即便依赖写成 `libraries["spring-core"]`(akasha 注入版本),在 Gradle 内部它已被解析为带 `group:name` 的 `ExternalModuleDependency`,脚本照样能拿到 GA——这是"跑进 Gradle"相对静态解析的决定性优势。

### 2.1 提取用 init script(`das.gradle`)

```groovy
import org.gradle.api.artifacts.ProjectDependency
import org.gradle.api.artifacts.ExternalModuleDependency
import groovy.json.JsonOutput

gradle.projectsEvaluated {
    def out = [subprojects: [], edges: []]

    rootProject.allprojects.each { p ->
        // ---- 坐标 GA ----
        def artifact = p.name                         // 默认 artifact = 项目名
        def pub = p.extensions.findByName('publishing')
        if (pub) {                                     // 用 maven-publish 时以 publication artifactId 为准
            try {
                def mp = pub.publications.find { it.hasProperty('artifactId') }
                if (mp) artifact = mp.artifactId
            } catch (ignored) {}
        }
        out.subprojects << [
            gradle_path: p.path,                       // ":core:api"
            group      : p.group?.toString(),
            artifact   : artifact,
            version    : p.version?.toString()
        ]

        // ---- 声明依赖(只读 declared,不触发解析) ----
        def seen = [] as Set
        p.configurations.each { c ->
            if (c.name.toLowerCase().contains('test')) return    // 跳过测试配置
            c.dependencies.each { d ->
                if (d instanceof ProjectDependency) {            // 项目内依赖
                    def path = d.hasProperty('path') ? d.path : d.dependencyProject.path
                    if (seen.add("P:$path"))
                        out.edges << [from: path, to: p.path, type: 'project']
                } else if (d instanceof ExternalModuleDependency) {  // 二方/三方依赖
                    if (seen.add("E:${d.group}:${d.name}"))
                        out.edges << [from: "${d.group}:${d.name}:${d.version}", to: p.path, type: 'external']
                }
            }
        }
    }
    new File(gradle.rootProject.projectDir, 'das-output.json').text = JsonOutput.toJson(out)
}
```

要点:
- 用 `configuration.dependencies`(**已声明依赖集**),不是 `dependencies` task 的解析结果——后者拉传递闭包、走网络、展开三方,我们都不需要。
- `ProjectDependency` / `ExternalModuleDependency` 是 Gradle API 层的天然区分,直接对应"项目内边 / 跨项目边"。
- 触发方式用最轻的 `help -q`,只完成 configuration 阶段(`projectsEvaluated` 即触发脚本),不编译、不执行业务任务。
- `ProjectDependency.dependencyProject` 在 Gradle 8.11+ 废弃,已优先 `d.path` 并回退兼容。

### 2.2 DAS 每仓库原始输出

```json
{
  "repo_id": "repo-0012", "tag": "v2026.03",
  "subprojects": [
    {"gradle_path": ":core:api",   "group": "com.csdc.spot", "artifact": "issuance-core-api"},
    {"gradle_path": ":core:model", "group": "com.csdc.spot", "artifact": "issuance-core-model"}
  ],
  "edges": [
    {"from": ":core:model",                          "to": ":core:api",   "type": "project"},
    {"from": "com.csdc.settle:settle-client:1.4.0",   "to": ":core:api",   "type": "external"},
    {"from": "org.springframework:spring-core:6.1.0", "to": ":core:model", "type": "external"}
  ]
}
```

注意原始边的 `from` 形式不统一(项目内是 gradlePath、跨项目是 GAV),**归一化是 GPS 的责任**(§4)。

## 3. K8s 执行方案

分析对每个 repo 是无状态、一次性、可并行、可重试的批任务,因此用 **K8s Job + 回调** 模型,每个 repo 一个 Job。

### 3.1 架构

```
GPS/DAS (Go)                          K8s 集群
   │  对每个 repo 渲染并创建 Job (repo_id + tag + akasha分支)
   ├──────────────────────────────────────────────▶  Job: das-<repo>-<tag>
   │                                                    initContainer: git clone --branch <tag>
   │                                                    container(openjdk+git): gradlew --init-script
   │                                                       └ apply from akasha (取 ext.libraries 版本)
   │                                                       └ 写 das-output.json
   │  ◀───────── POST /das/callback (das-output.json) ─────┘ (Job 内 curl 回传)
   │  收齐所有 repo → 归一化 → GA 图 + 环检测 → 落库
```

选 Job 而非常驻 Pod 的原因:批任务语义吻合,可用 `backoffLimit`、`activeDeadlineSeconds`、`ttlSecondsAfterFinished` 管理重试/超时/清理。

### 3.2 镜像:职责分离,不污染 Java 容器

代码拉取与依赖分析用**两个独立镜像**,经共享卷传递代码,使分析容器保持纯净(不含 git/ssh):

**(a) clone 镜像(initContainer 用)**——纯 git/ssh 工具,不含 JDK:

```dockerfile
# registry/gps-das-git:latest
FROM alpine/git:latest        # 已含 git + openssh;或 FROM alpine + apk add git openssh-client
# 无需额外内容,克隆逻辑由 Job 的 command 提供
```

**(b) analyze 镜像(主容器用)**——纯净 openjdk,**不装 git、不装 ssh**:

```dockerfile
# registry/gps-das-jdk:latest
FROM eclipse-temurin:17-jdk           # 或 openjdk:17-jdk,保持官方镜像的纯净
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && \
    rm -rf /var/lib/apt/lists/*       # 仅加 curl 用于回传;不引入 git/openssh
ENV GRADLE_USER_HOME=/gradle-cache    # 预置/缓存 Gradle 发行版,避免每个 Job 重新下载 wrapper dist
WORKDIR /work
```

要点:
- **拉代码(git/ssh)只存在于 init 镜像**,Java 镜像与版本控制工具彻底解耦,符合"init 容器准备数据、主容器只跑业务"的 K8s 惯例。
- analyze 镜像只在官方 openjdk 上加了一个 `curl`(用于回传结果)。若回传也想剥离,可再拆一个 sidecar/后置 init 容器,但通常 curl 足够轻量、可接受;git/ssh 才是真正需要隔离的重量级污染。
- 两镜像各自精简,init 镜像极小、启动快;analyze 镜像无关工具最少,攻击面更小。

> gradlew(wrapper)首次运行会联网下载 Gradle 发行版。要么 analyze 镜像里预置 `GRADLE_USER_HOME`,要么挂只读 gradle-dist 缓存卷(§3.5)。

### 3.3 init script 用 ConfigMap 注入

把 `das.gradle` 放进 ConfigMap 挂载,迭代脚本无需重打镜像:

```yaml
apiVersion: v1
kind: ConfigMap
metadata: { name: das-init-script }
data:
  das.gradle: |
    # §2.1 的提取脚本
```

### 3.4 Job 模板(GPS 渲染后创建)

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: das-{{repo_id}}-{{tag}}
  labels: { app: gps-das, plan: "{{plan_id}}" }
spec:
  backoffLimit: 1                 # 失败重试 1 次
  activeDeadlineSeconds: 600      # 单 repo 超时 10min
  ttlSecondsAfterFinished: 600    # 完成后 10min 自动清理
  template:
    spec:
      restartPolicy: Never
      volumes:
        - { name: workspace,    emptyDir: {} }
        - { name: init-script,  configMap: { name: das-init-script } }
        - { name: ssh-key,      secret: { secretName: codeup-ssh, defaultMode: 0400 } }
        - { name: gradle-cache, persistentVolumeClaim: { claimName: gradle-dist-cache } }  # 只读发行版缓存
      initContainers:
        - name: clone                       # 1. 克隆 tag(浅克隆,只读)— 纯 git 镜像
          image: registry/gps-das-git:latest
          command: ["sh","-c"]
          args:
            - |
              export GIT_SSH_COMMAND="ssh -i /keys/id_rsa -o StrictHostKeyChecking=no"
              git clone --depth 1 --branch {{tag}} {{repo_url}} /work/src
          volumeMounts:
            - { name: workspace, mountPath: /work }
            - { name: ssh-key,   mountPath: /keys }
      containers:
        - name: analyze                     # 2. 评估 Gradle 模型 — 纯净 openjdk 镜像(无 git/ssh)
          image: registry/gps-das-jdk:latest
          workingDir: /work/src
          env:
            - { name: DEP_BRANCH,        value: "{{akasha_branch}}" }   # gradle.properties: depBranch
            - { name: GRADLE_USER_HOME,  value: /gradle-cache }
          command: ["sh","-c"]
          args:
            - |
              ./gradlew --init-script /scripts/das.gradle \
                        -PdepBranch=$DEP_BRANCH \
                        --offline help -q || true   # 只评估;若 akasha apply 需联网则去掉 --offline
              curl -sf -X POST \
                "http://gps-das.gps.svc/das/callback?repo_id={{repo_id}}&tag={{tag}}&plan_id={{plan_id}}" \
                -H "Content-Type: application/json" \
                --data-binary @/work/src/das-output.json
          volumeMounts:
            - { name: workspace,    mountPath: /work }
            - { name: init-script,  mountPath: /scripts }
            - { name: gradle-cache, mountPath: /gradle-cache }
          resources:
            requests: { cpu: "500m", memory: "1Gi" }
            limits:   { cpu: "2",    memory: "2Gi" }
```

要点:
- **职责分离**:init 容器(`gps-das-git`)拉代码,主容器(`gps-das-jdk`)只评估 Gradle,二者经 `emptyDir` 共享 `/work`;git/ssh 不进 Java 容器。
- ssh-key Secret 只挂给 init 容器,主容器不接触凭据。
- **结果回传用 `curl POST` 到 DAS 回调端点**(带 repo_id+tag+plan_id),比读 Pod 日志稳健(日志会被 Gradle 噪声污染、有大小限制)。
- `help -q` 只完成 configuration 阶段即触发脚本,不进入编译/业务执行。

### 3.5 网络与离线权衡(关键)

| 需求 | 是否需要网络 | 处理 |
|------|-------------|------|
| 拉 git tag | 需(ssh 到 codeup) | ssh key Secret + `StrictHostKeyChecking=no`(自签名/内网) |
| `apply from: akasha/dependency?branch=` | **需**(取 `ext.libraries` 才能拿到 group:name) | akasha 同集群,走 service DNS,`fromInsecureUri` 用 http |
| 解析传递依赖 / 访问 Maven 仓库 | **不需** | 只读 declared 依赖,可 `--offline` |
| 下载 Gradle wrapper dist | 需(除非缓存) | 预置 `gradle-dist-cache` PVC(ReadOnlyMany)或烤进镜像 |

结论:**半离线**——Maven 解析关闭(`--offline`),但 akasha 必须可达。若把 akasha 清单预先下载成本地文件注入,可做到完全离线;通常同集群直连 akasha 更简单。

## 4. GPS 侧归一化(汇总后,纯逻辑,不碰 Gradle)

收齐所有 repo 的 `das-output.json` 后:

1. 用每个 repo 的 `subprojects` 建表 `gradlePath → GA`(GA = `group:artifact`)。
2. 逐边把两端解析为 GA:
   - `type=project` → `from` 是 gradlePath,查**本 repo**表 → GA;
   - `type=external` → 剥掉版本,`group:name` 即 GA。
3. 给每个 GA 节点分类:
   - **internal**:命中某 devops repo 的 subproject;
   - **pending-external**:GA 属自研 group 命名空间(如 `com.csdc.*`)但不命中任何 devops repo;
   - **third-party**:其余(如 `org.springframework:*`)→ **丢弃,不进图**。
4. `cross_repo = (from 与 to 属于不同 repo)`。
5. 拓扑排序 + 环检测(模块级有环则 Phase 2 失败并定位环路径)。

`artifact` 即 akasha 的 join key;同一 akasha 分支内 artifact 须唯一,归一化时若发现冲突报错。

## 5. 编排逻辑(DAS 服务)

```
analyzePlan(plan_id, repos[], akashaBranch):
  for repo in repos:                       # 受 maxParallel 信号量限制
     renderJobYAML(repo, akashaBranch) → k8s.CreateJob()
  收集:每个 repo 的 das-output.json 经 /das/callback 收齐
        (并 watch Job.status;超时/失败的 repo 标记 analysis_failed)
  归一化所有 subprojects+edges → GA 图(§4)
  拓扑 + 环检测
  落 plan_module / plan_dep_edge / plan_topo_order(§6)
```

- 并发上限:DAS 内信号量控制同时创建的 Job 数(也可配 ResourceQuota 兜底)。
- 失败处理:某 repo Job 失败/超时 → 标 `analysis_failed`,整个 Phase 2 失败并指出是哪个 repo;支持单 repo 重跑。
- 幂等:Job 名含 `repo_id+tag`,重复创建先删后建或加随机后缀。

### 5.1 RBAC 与 Secret

```yaml
# git ssh 私钥
apiVersion: v1
kind: Secret
metadata: { name: codeup-ssh }
type: Opaque
data: { id_rsa: <base64 私钥> }
---
# DAS 创建 Job 的权限(in-cluster ServiceAccount)
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: { namespace: gps, name: das-job-runner }
rules:
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["create","get","list","watch","delete"]
  - apiGroups: [""]
    resources: ["pods","pods/log"]
    verbs: ["get","list"]
```

## 6. 落库 schema(计划级快照)

依赖分析结果是计划级快照(不同 plan / tag 可能不同图),全部表带 `plan_id`。

### 6.1 `plan_module` — 节点(模块)

```sql
CREATE TABLE plan_module (
    plan_id        VARCHAR(64)  NOT NULL,
    ga             VARCHAR(255) NOT NULL,   -- group:artifact,节点主键
    group_id       VARCHAR(128) NOT NULL,
    artifact       VARCHAR(128) NOT NULL,   -- = akasha join key
    kind           VARCHAR(24)  NOT NULL,   -- internal | pending-external
    repo_id        VARCHAR(64),             -- pending-external 为 NULL
    silo_id        VARCHAR(64),
    target_version VARCHAR(32),             -- internal:本次版本; pending-external:akasha 确认版本
    PRIMARY KEY (plan_id, ga)
);
```

| plan_id | ga | artifact | kind | repo_id | target_version |
|---|---|---|---|---|---|
| plan-001 | `com.csdc.spot:issuance-core-api` | issuance-core-api | internal | repo-0012 | 2026.03 |
| plan-001 | `com.csdc.spot:issuance-core-model` | issuance-core-model | internal | repo-0012 | 2026.03 |
| plan-001 | `com.csdc.settle:settle-client` | settle-client | internal | repo-0007 | 2026.03 |
| plan-001 | `com.csdc.legacy:foo` | foo | pending-external | NULL | 3.1.0 |

### 6.2 `plan_dep_edge` — 边

```sql
CREATE TABLE plan_dep_edge (
    plan_id    VARCHAR(64)  NOT NULL,
    from_ga    VARCHAR(255) NOT NULL,   -- 被依赖方(上游)
    to_ga      VARCHAR(255) NOT NULL,   -- 依赖方(下游)
    cross_repo BOOLEAN      NOT NULL,   -- true=跨repo(经akasha) / false=repo内
    PRIMARY KEY (plan_id, from_ga, to_ga)
);
```

| plan_id | from_ga | to_ga | cross_repo |
|---|---|---|---|
| plan-001 | `com.csdc.spot:issuance-core-model` | `com.csdc.spot:issuance-core-api` | false |
| plan-001 | `com.csdc.settle:settle-client` | `com.csdc.spot:issuance-core-api` | true |
| plan-001 | `com.csdc.legacy:foo` | `com.csdc.spot:issuance-core-model` | true |

### 6.3 `plan_gradle_subproject` — 归一化映射(审计/回溯,可选)

```sql
CREATE TABLE plan_gradle_subproject (
    plan_id     VARCHAR(64)  NOT NULL,
    repo_id     VARCHAR(64)  NOT NULL,
    gradle_path VARCHAR(255) NOT NULL,   -- ":core:api"
    ga          VARCHAR(255) NOT NULL,
    PRIMARY KEY (plan_id, repo_id, gradle_path)
);
```

### 6.4 `plan_topo_order` — 排序结果

```sql
CREATE TABLE plan_topo_order (
    plan_id   VARCHAR(64)  NOT NULL,
    seq       INT          NOT NULL,
    ga        VARCHAR(255) NOT NULL,
    PRIMARY KEY (plan_id, seq)
);
```

## 7. 边界情形

| 情形 | 处理 |
|---|---|
| artifact ≠ 项目名(archivesBaseName / publication artifactId) | 优先读 publication 的 artifactId(init script 已处理) |
| `platform()/enforcedPlatform()` BOM | 也是 ExternalModuleDependency;按 group 分类,通常落 third-party 被丢弃 |
| 测试依赖(testImplementation) | 已按配置名含 `test` 跳过,不影响发布顺序 |
| 仅 repo 内引用、从不发布的子项目 | 仍生成 GA 进图,标 repo-internal,不回写 akasha |
| `dependencyConstraints` / 版本对齐 | 非真实依赖,不取 |
| composite build(`includeBuild`) | 罕见;按 external 处理或单列为暂不支持,需另议 |
| 同分支内 artifact 冲突 | 归一化阶段报错(join key 歧义) |
| Gradle wrapper 版本不一 | gradle-dist 缓存按版本共存 |

## 8. 为什么这条路对

- **权威**:group/version/依赖坐标最终只有 Gradle 自己说了算,静态文本解析在 version catalog / 远程 apply / 条件依赖面前必失真。
- **快且安全**:只评估 + 读 declared 依赖,不编译、不解析传递闭包、可 `--offline`,对 tag 工作区只读。
- **天然分类**:Gradle API 直接区分 project/external 依赖,省去猜测项目内/跨项目。
- **与 akasha 闭环**:分析阶段就 `apply from: akasha`,与发布阶段注入的依赖版本同源,口径一致。

## 9. 与 GPS 主流程的衔接

- 本方案是 `design.md` §5.2 DAS 的真实实现,被 GPS **Phase 2(依赖分析与拓扑排序)** 调用。
- GPS 主程序不直接操作 K8s;DAS 作为独立服务(in-cluster config)负责拉起、监控、回收 Job,并把归一化后的图返回 GPS。
- 产出的 `plan_module / plan_dep_edge / plan_topo_order` 供 Phase 2.5(pending-external 确认)与 Phase 3(并发池发布)消费。

> 本文档为详细设计,具体编码在后续迭代落地。
