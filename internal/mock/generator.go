package mock

import (
	"fmt"
	"gps/internal/model"
	"math/rand"
)

// siloDefs defines 30 silos with business domain names
var siloDefs = []struct {
	name string
	desc string
}{
	{"payment", "支付核心服务"},
	{"trading", "交易引擎"},
	{"risk", "风控系统"},
	{"settlement", "清结算"},
	{"user", "用户中心"},
	{"merchant", "商户管理"},
	{"notification", "通知服务"},
	{"reporting", "报表系统"},
	{"compliance", "合规审计"},
	{"gateway", "网关服务"},
	{"billing", "计费系统"},
	{"ledger", "账本服务"},
	{"pricing", "定价引擎"},
	{"catalog", "商品目录"},
	{"inventory", "库存管理"},
	{"order", "订单系统"},
	{"shipping", "物流服务"},
	{"returns", "退货管理"},
	{"loyalty", "积分系统"},
	{"analytics", "数据分析"},
	{"search", "搜索服务"},
	{"recommendation", "推荐引擎"},
	{"auth", "认证授权"},
	{"config", "配置中心"},
	{"monitoring", "监控系统"},
	{"logging", "日志平台"},
	{"messaging", "消息队列"},
	{"scheduler", "任务调度"},
	{"workflow", "工作流引擎"},
	{"audit", "审计追踪"},
}

// repoTemplates defines how repos are structured within a silo
var repoTemplates = [][]string{
	{"core"},
	{"core", "gateway"},
	{"core", "api"},
	{"service"},
	{"service", "common"},
	{"core", "adapter"},
}

// moduleSuffixes defines module name suffixes within a repo
var moduleSuffixes = [][]string{
	{"model", "api"},
	{"model", "api", "service"},
	{"model", "api", "service"},
	{"model", "core"},
	{"common", "client"},
	{"model", "api", "service", "adapter"},
	{"client", "common"},
	{"model", "client", "service"},
}

// GenerateData creates deterministic mock data with pure (from, to) edge tuples
func GenerateData() ([]model.Silo, []model.Repo, []model.Module, []model.DepEdge) {
	rng := rand.New(rand.NewSource(42))

	var silos []model.Silo
	var repos []model.Repo
	var modules []model.Module

	for i, sd := range siloDefs {
		silo := model.Silo{
			ID:   fmt.Sprintf("silo-%03d", i+1),
			Name: sd.name,
			Desc: sd.desc,
		}
		silos = append(silos, silo)

		tmplIdx := rng.Intn(len(repoTemplates))
		repoNames := repoTemplates[tmplIdx]
		if len(repos) > 40 && len(repoNames) > 1 {
			repoNames = repoNames[:1]
		}

		for _, rn := range repoNames {
			repoName := fmt.Sprintf("%s-%s", sd.name, rn)
			branches := []string{"release/2025Q2", "release/2025Q3", "main", "develop"}
			repo := model.Repo{
				ID:            fmt.Sprintf("repo-%03d", len(repos)+1),
				SiloID:        silo.ID,
				Name:          repoName,
				URL:           fmt.Sprintf("git@gitlab.internal.com:platform/%s.git", repoName),
				ReleaseBranch: branches[rng.Intn(len(branches))],
			}
			repos = append(repos, repo)

			specIdx := rng.Intn(len(moduleSuffixes))
			suffixes := moduleSuffixes[specIdx]
			if len(modules) > 85 && len(suffixes) > 2 {
				suffixes = suffixes[:2]
			}
			if len(modules) > 95 {
				suffixes = suffixes[:1]
			}

			for _, sfx := range suffixes {
				modName := fmt.Sprintf("%s-%s", repoName, sfx)
				mod := model.Module{
					ID:     fmt.Sprintf("mod-%03d", len(modules)+1),
					RepoID: repo.ID,
					SiloID: silo.ID,
					Name:   modName,
				}
				modules = append(modules, mod)
			}
		}
	}

	assignVersions(modules, rng)
	edges := generateEdges(modules, rng)

	return silos, repos, modules, edges
}

func assignVersions(modules []model.Module, rng *rand.Rand) {
	// Same repo shares the same version (repo-level tag)
	repoVersions := make(map[string]string)
	for i := range modules {
		rid := modules[i].RepoID
		if _, ok := repoVersions[rid]; !ok {
			major := 1 + rng.Intn(4)
			minor := rng.Intn(6)
			patch := 1 + rng.Intn(20)
			repoVersions[rid] = fmt.Sprintf("%d.%d.%d", major, minor, patch)
		}
		modules[i].CurrentVersion = repoVersions[rid]
	}
}

// generateEdges produces pure (from, to) dependency tuples.
// DAG is guaranteed by only allowing edges from lower-index modules to higher-index modules.
func generateEdges(modules []model.Module, rng *rand.Rand) []model.DepEdge {
	n := len(modules)
	edgeSet := make(map[string]bool)
	var edges []model.DepEdge

	addEdge := func(fromIdx, toIdx int) {
		if fromIdx == toIdx || fromIdx >= n || toIdx >= n {
			return
		}
		// Enforce DAG: from must have lower index than to
		if fromIdx > toIdx {
			return
		}
		key := modules[fromIdx].ID + "->" + modules[toIdx].ID
		if !edgeSet[key] {
			edges = append(edges, model.DepEdge{From: modules[fromIdx].ID, To: modules[toIdx].ID})
			edgeSet[key] = true
		}
	}

	// 1. Each module (except the first ~15%) has a 40% chance of depending on
	//    a random earlier module. This creates the basic DAG structure.
	for i := 1; i < n; i++ {
		if rng.Intn(100) < 40 {
			dep := rng.Intn(i) // pick a random module before this one
			addEdge(dep, i)
		}
	}

	// 2. Create some "hub" modules that many others depend on (simulates
	//    shared libraries like auth-common, config-model, messaging-api).
	//    Pick ~5 modules from the first 20% as hubs.
	hubCount := 5
	firstChunk := n * 20 / 100
	if firstChunk < hubCount {
		firstChunk = hubCount
	}
	hubs := rng.Perm(firstChunk)
	if len(hubs) > hubCount {
		hubs = hubs[:hubCount]
	}
	for _, hubIdx := range hubs {
		// ~25% of modules after this hub depend on it
		for j := hubIdx + 1; j < n; j++ {
			if rng.Intn(100) < 25 {
				addEdge(hubIdx, j)
			}
		}
	}

	// 3. Ensure at least some connectivity: every module beyond index 5
	//    that has zero in-edges gets one random dependency on an earlier module.
	inDeg := make([]int, n)
	for _, e := range edges {
		for j, m := range modules {
			if m.ID == e.To {
				inDeg[j]++
				break
			}
		}
	}
	for i := 5; i < n; i++ {
		if inDeg[i] == 0 {
			dep := rng.Intn(i)
			addEdge(dep, i)
		}
	}

	return edges
}

// TopologicalSort performs Kahn's algorithm on the given modules/edges
func TopologicalSort(moduleIDs []string, edges []model.DepEdge) []string {
	inDegree := make(map[string]int)
	adjacency := make(map[string][]string)
	idSet := make(map[string]bool)

	for _, id := range moduleIDs {
		inDegree[id] = 0
		idSet[id] = true
	}

	for _, e := range edges {
		if idSet[e.From] && idSet[e.To] {
			adjacency[e.From] = append(adjacency[e.From], e.To)
			inDegree[e.To]++
		}
	}

	var queue []string
	for _, id := range moduleIDs {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		for _, next := range adjacency[node] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	return sorted
}
