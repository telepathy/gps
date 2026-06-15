package mock

import (
	"fmt"
	"gps/internal/model"
	"math/rand"
	"strings"
)

// moduleSuffixes defines module name suffixes within a repo.
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

// GenerateModulesForPlan synthesizes modules with GA identity for a plan.
// It produces internal modules (from the given repos) and a small number of
// pending-external modules (self-developed but not devops-enabled) to
// demonstrate the confirmation gate.
func GenerateModulesForPlan(repos []model.Repo, seed int64) []model.Module {
	rng := rand.New(rand.NewSource(seed))

	var modules []model.Module

	// Generate internal modules from repos.
	for _, repo := range repos {
		specIdx := rng.Intn(len(moduleSuffixes))
		suffixes := moduleSuffixes[specIdx]

		// Derive group from silo: com.csdc.<silo_code>
		group := fmt.Sprintf("com.csdc.%s", sanitizeName(repo.SiloID))

		for _, sfx := range suffixes {
			artifact := fmt.Sprintf("%s-%s", sanitizeName(repo.Name), sfx)
			ga := fmt.Sprintf("%s:%s", group, artifact)
			displayName := fmt.Sprintf("%s-%s", repo.Name, sfx)

			modules = append(modules, model.Module{
				ID:         ga,
				Group:      group,
				Artifact:   artifact,
				GradlePath: fmt.Sprintf(":%s", strings.ReplaceAll(sfx, "-", ":")),
				RepoID:     repo.ID,
				SiloID:     repo.SiloID,
				Name:       displayName,
				Kind:       model.KindInternal,
			})
		}
	}

	assignVersions(modules, rng)

	// Generate 2-3 pending-external modules.
	// These represent self-developed modules not in devops, depended on by internal modules.
	pendingCount := 2 + rng.Intn(2) // 2 or 3
	legacyGroup := "com.csdc.legacy"
	for i := 0; i < pendingCount; i++ {
		artifact := fmt.Sprintf("legacy-lib-%c", 'a'+rune(i))
		ga := fmt.Sprintf("%s:%s", legacyGroup, artifact)
		modules = append(modules, model.Module{
			ID:             ga,
			Group:          legacyGroup,
			Artifact:       artifact,
			GradlePath:     "",
			RepoID:         "",
			SiloID:         "",
			Name:           fmt.Sprintf("legacy-lib-%c", 'a'+rune(i)),
			Kind:           model.KindPendingExternal,
			CurrentVersion: fmt.Sprintf("1.%d.0", rng.Intn(5)+1), // pre-existing version in akasha
		})
	}

	return modules
}

// GenerateModulesForRepos is a convenience wrapper for startup-level generation
// (used when no specific plan exists). Returns internal modules only.
func GenerateModulesForRepos(repos []model.Repo) []model.Module {
	return GenerateModulesForPlan(repos, 42)
}

func assignVersions(modules []model.Module, rng *rand.Rand) {
	repoVersions := make(map[string]string)
	for i := range modules {
		if modules[i].Kind != model.KindInternal {
			continue
		}
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

// GenerateEdges produces dependency edges for the given modules.
// Edges distinguish cross-repo (via akasha) vs repo-internal.
// Pending-external modules are used as targets of cross-repo edges.
func GenerateEdges(modules []model.Module) []model.DepEdge {
	rng := rand.New(rand.NewSource(42))

	// Build lookup maps.
	byRepo := make(map[string][]int) // repoID -> indices
	for i, m := range modules {
		byRepo[m.RepoID] = append(byRepo[m.RepoID], i)
	}

	// Separate internal and pending-external.
	var internalIdxs []int
	var pendingIdxs []int
	for i, m := range modules {
		switch m.Kind {
		case model.KindInternal:
			internalIdxs = append(internalIdxs, i)
		case model.KindPendingExternal:
			pendingIdxs = append(pendingIdxs, i)
		}
	}

	n := len(modules)
	edgeSet := make(map[string]bool)
	var edges []model.DepEdge

	addEdge := func(fromIdx, toIdx int) {
		if fromIdx == toIdx || fromIdx >= n || toIdx >= n {
			return
		}
		if fromIdx > toIdx {
			return // enforce DAG
		}
		from := modules[fromIdx]
		to := modules[toIdx]
		key := from.ID + "->" + to.ID
		if edgeSet[key] {
			return
		}
		crossRepo := from.RepoID != to.RepoID || from.RepoID == "" || to.RepoID == ""
		edges = append(edges, model.DepEdge{
			From:      from.ID,
			To:        to.ID,
			CrossRepo: crossRepo,
		})
		edgeSet[key] = true
	}

	// 1. Repo-internal edges: modules within the same repo.
	for _, idxs := range byRepo {
		for i := 1; i < len(idxs); i++ {
			if rng.Intn(100) < 50 {
				addEdge(idxs[0], idxs[i])
			}
		}
	}

	// 2. Cross-repo edges between internal modules.
	for _, toIdx := range internalIdxs {
		if rng.Intn(100) < 30 {
			// Pick a random earlier internal module from a different repo.
			candidates := filterByRepo(internalIdxs[:toIdx], modules, modules[toIdx].RepoID, true)
			if len(candidates) > 0 {
				fromIdx := candidates[rng.Intn(len(candidates))]
				addEdge(fromIdx, toIdx)
			}
		}
	}

	// 3. Cross-repo edges from pending-external to internal modules.
	//    Each pending-external module is depended on by 1-3 internal modules.
	for _, pIdx := range pendingIdxs {
		dependents := 1 + rng.Intn(3)
		shuffled := make([]int, len(internalIdxs))
		copy(shuffled, internalIdxs)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		count := 0
		for _, toIdx := range shuffled {
			if count >= dependents {
				break
			}
			// pending-external may have lower or higher index; allow either direction
			// by not enforcing fromIdx < toIdx for external deps.
			key := modules[pIdx].ID + "->" + modules[toIdx].ID
			if !edgeSet[key] {
				edges = append(edges, model.DepEdge{
					From:      modules[pIdx].ID,
					To:        modules[toIdx].ID,
					CrossRepo: true,
				})
				edgeSet[key] = true
				count++
			}
		}
	}

	// 4. Hub edges: pick ~3 internal modules as hubs.
	hubCount := 3
	if len(internalIdxs) < hubCount {
		hubCount = len(internalIdxs)
	}
	hubs := rng.Perm(len(internalIdxs))[:hubCount]
	for _, hi := range hubs {
		hubIdx := internalIdxs[hi]
		for _, toIdx := range internalIdxs {
			if toIdx > hubIdx && rng.Intn(100) < 20 {
				addEdge(hubIdx, toIdx)
			}
		}
	}

	// 5. Ensure connectivity for internal modules beyond the first 3.
	inDeg := make(map[string]int)
	for _, e := range edges {
		inDeg[e.To]++
	}
	start := 3
	if start > len(internalIdxs) {
		start = len(internalIdxs)
	}
	for _, idx := range internalIdxs[start:] {
		if inDeg[modules[idx].ID] == 0 {
			candidates := filterBefore(internalIdxs, idx)
			if len(candidates) > 0 {
				fromIdx := candidates[rng.Intn(len(candidates))]
				addEdge(fromIdx, idx)
			}
		}
	}

	return edges
}

// sanitizeName converts a string to a valid Maven artifact segment.
func sanitizeName(s string) string {
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return strings.ToLower(s)
}

// filterByRepo returns indices whose module is NOT in the given repo (if exclude=true)
// or IS in the given repo (if exclude=false).
func filterByRepo(idxs []int, modules []model.Module, repoID string, exclude bool) []int {
	var result []int
	for _, i := range idxs {
		if (modules[i].RepoID != repoID) == exclude {
			result = append(result, i)
		}
	}
	return result
}

// filterBefore returns indices that are less than the given index.
func filterBefore(idxs []int, before int) []int {
	var result []int
	for _, i := range idxs {
		if i < before {
			result = append(result, i)
		}
	}
	return result
}

// TopologicalSort re-exports model.TopologicalSort for backward compatibility.
func TopologicalSort(moduleIDs []string, edges []model.DepEdge) ([]string, bool, []string) {
	return model.TopologicalSort(moduleIDs, edges)
}
