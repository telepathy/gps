package mock

import (
	"fmt"
	"gps/internal/model"
	"math/rand"
	"strings"
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

// moduleSpec: suffix -> layer assignment within a repo
// Layer determined by module type suffix (semantic, not positional)
var moduleSpecs = []struct {
	suffixes []string // module name suffixes within repo
}{
	{[]string{"model", "api"}},
	{[]string{"model", "api", "service"}},
	{[]string{"model", "api", "service"}},
	{[]string{"model", "core"}},
	{[]string{"common", "client"}},
	{[]string{"model", "api", "service", "adapter"}},
	{[]string{"client", "common"}},
	{[]string{"model", "client", "service"}},
}

// suffixToLayer maps module type suffix to a base layer
// model/common = 0, api/client = 1, core/service = 2, adapter/gateway = 3
var suffixToLayer = map[string]int{
	"model":   0,
	"common":  0,
	"api":     1,
	"client":  1,
	"core":    2,
	"service": 2,
	"adapter": 3,
	"gateway": 3,
}

// siloLayerBoost: certain "infrastructure" silos get lower layers,
// "application" silos get higher layers. This creates cross-silo deps.
var infraSilos = map[string]bool{
	"auth": true, "config": true, "logging": true,
	"messaging": true, "monitoring": true,
}
var midSilos = map[string]bool{
	"user": true, "payment": true, "ledger": true,
	"billing": true, "gateway": true, "scheduler": true,
}

// highSilos: everything else (order, shipping, analytics, etc.) are high-layer

// GenerateData creates deterministic mock data
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

		// Pick 1-3 repos per silo using deterministic template
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

			specIdx := rng.Intn(len(moduleSpecs))
			suffixes := moduleSpecs[specIdx].suffixes
			if len(modules) > 85 && len(suffixes) > 2 {
				suffixes = suffixes[:2]
			}
			if len(modules) > 95 {
				suffixes = suffixes[:1]
			}

			for _, sfx := range suffixes {
				modName := fmt.Sprintf("%s-%s", repoName, sfx)

				// Compute layer: base from suffix + boost from silo type
				baseLayer := suffixToLayer[sfx]
				siloBoost := 0
				if infraSilos[sd.name] {
					siloBoost = 0
				} else if midSilos[sd.name] {
					siloBoost = 1
				} else {
					siloBoost = 2
				}
				layer := baseLayer + siloBoost
				if layer > 4 {
					layer = 4
				}

				mod := model.Module{
					ID:     fmt.Sprintf("mod-%03d", len(modules)+1),
					RepoID: repo.ID,
					SiloID: silo.ID,
					Name:   modName,
					Layer:  layer,
				}
				modules = append(modules, mod)
			}
		}
	}

	assignVersions(modules, rng)
	edges := generateDependencies(modules, rng)

	return silos, repos, modules, edges
}

func assignVersions(modules []model.Module, rng *rand.Rand) {
	for i := range modules {
		switch modules[i].Layer {
		case 0:
			modules[i].CurrentVersion = fmt.Sprintf("%d.%d.%d", 3+rng.Intn(3), rng.Intn(5), rng.Intn(20)+1)
		case 1:
			modules[i].CurrentVersion = fmt.Sprintf("%d.%d.%d", 2+rng.Intn(2), rng.Intn(5), rng.Intn(15)+1)
		case 2:
			modules[i].CurrentVersion = fmt.Sprintf("%d.%d.%d", 1+rng.Intn(2), rng.Intn(5), rng.Intn(10)+1)
		case 3:
			modules[i].CurrentVersion = fmt.Sprintf("%d.%d.%d", 1, rng.Intn(3), rng.Intn(10)+1)
		case 4:
			modules[i].CurrentVersion = fmt.Sprintf("0.%d.%d", 1+rng.Intn(5), rng.Intn(10)+1)
		}
	}
}

func generateDependencies(modules []model.Module, rng *rand.Rand) []model.DepEdge {
	var edges []model.DepEdge
	edgeSet := make(map[string]bool)

	// Build module index
	modIndex := make(map[string]int) // id -> index
	for i, m := range modules {
		modIndex[m.ID] = i
	}

	// addEdge enforces DAG: from.Layer <= to.Layer, and if same layer, from.index < to.index
	addEdge := func(fromID, toID string) {
		if fromID == toID {
			return
		}
		fi, ti := modIndex[fromID], modIndex[toID]
		fLayer, tLayer := modules[fi].Layer, modules[ti].Layer
		// Strict: from layer must be < to layer, OR same layer but from has lower global index
		if fLayer > tLayer {
			return
		}
		if fLayer == tLayer && fi >= ti {
			return
		}
		key := fromID + "->" + toID
		if !edgeSet[key] {
			edges = append(edges, model.DepEdge{From: fromID, To: toID})
			edgeSet[key] = true
		}
	}

	// Group modules by layer
	layerModules := make(map[int][]int)
	for i, m := range modules {
		layerModules[m.Layer] = append(layerModules[m.Layer], i)
	}

	// Group modules by repo for intra-repo deps
	repoModules := make(map[string][]int)
	for i, m := range modules {
		repoModules[m.RepoID] = append(repoModules[m.RepoID], i)
	}

	// 1. Intra-repo chain: model -> api -> service -> adapter within same repo
	for _, indices := range repoModules {
		for j := 1; j < len(indices); j++ {
			addEdge(modules[indices[j-1]].ID, modules[indices[j]].ID)
		}
	}

	// 2. Cross-layer dependencies: each module in L1+ depends on 1-2 modules from strictly lower layers
	for layer := 1; layer <= 4; layer++ {
		for _, idx := range layerModules[layer] {
			var candidates []int
			for l := 0; l < layer; l++ {
				candidates = append(candidates, layerModules[l]...)
			}
			if len(candidates) == 0 {
				continue
			}

			numDeps := 1 + rng.Intn(2)
			if numDeps > len(candidates) {
				numDeps = len(candidates)
			}

			perm := rng.Perm(len(candidates))
			for d := 0; d < numDeps; d++ {
				addEdge(modules[candidates[perm[d]]].ID, modules[idx].ID)
			}
		}
	}

	// 3. Cross-silo infrastructure deps
	var infraModIDs []string
	for _, m := range modules {
		siloName := ""
		for _, s := range siloDefs {
			if strings.HasPrefix(m.Name, s.name+"-") {
				siloName = s.name
				break
			}
		}
		if infraSilos[siloName] && m.Layer <= 1 {
			infraModIDs = append(infraModIDs, m.ID)
		}
	}

	if len(infraModIDs) > 0 {
		for _, m := range modules {
			siloName := ""
			for _, s := range siloDefs {
				if strings.HasPrefix(m.Name, s.name+"-") {
					siloName = s.name
					break
				}
			}
			if infraSilos[siloName] {
				continue
			}
			if m.Layer < 2 {
				continue
			}
			if rng.Intn(100) < 30 {
				pick := infraModIDs[rng.Intn(len(infraModIDs))]
				addEdge(pick, m.ID)
			}
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
