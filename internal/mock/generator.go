package mock

import (
	"fmt"
	"gps/internal/model"
	"math/rand"
)

// moduleSuffixes defines module name suffixes within a repo. Silo and repo data
// come from dalaran; only modules and the dependency graph are synthesized here.
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

// GenerateModulesForRepos deterministically synthesizes the module set and the
// dependency DAG for a list of repos (sourced from dalaran). Module info from
// dalaran is intentionally not used — GPS owns the module-level release graph.
func GenerateModulesForRepos(repos []model.Repo) ([]model.Module, []model.DepEdge) {
	rng := rand.New(rand.NewSource(42))

	var modules []model.Module
	for _, repo := range repos {
		specIdx := rng.Intn(len(moduleSuffixes))
		suffixes := moduleSuffixes[specIdx]

		for _, sfx := range suffixes {
			modName := fmt.Sprintf("%s-%s", repo.Name, sfx)
			mod := model.Module{
				ID:     fmt.Sprintf("mod-%03d", len(modules)+1),
				RepoID: repo.ID,
				SiloID: repo.SiloID,
				Name:   modName,
			}
			modules = append(modules, mod)
		}
	}

	assignVersions(modules, rng)
	edges := generateEdges(modules, rng)
	return modules, edges
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
