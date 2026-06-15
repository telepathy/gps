package model

// TopologicalSort performs Kahn's algorithm on the given modules/edges.
// Returns the sorted GA list, whether a cycle was detected, and the cycle path
// (empty if no cycle).
func TopologicalSort(moduleIDs []string, edges []DepEdge) (sorted []string, hasCycle bool, cyclePath []string) {
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

	sorted = make([]string, 0, len(moduleIDs))
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

	if len(sorted) < len(moduleIDs) {
		// Cycle detected — find one cycle path.
		cyclePath = findCyclePath(moduleIDs, edges, sorted)
		return sorted, true, cyclePath
	}

	return sorted, false, nil
}

// findCyclePath finds one cycle in the graph by DFS from a remaining node.
func findCyclePath(moduleIDs []string, edges []DepEdge, sorted []string) []string {
	// Build the set of nodes involved in the cycle.
	inSorted := make(map[string]bool)
	for _, id := range sorted {
		inSorted[id] = true
	}

	// Build adjacency for remaining nodes only.
	adj := make(map[string][]string)
	remaining := make(map[string]bool)
	for _, id := range moduleIDs {
		if !inSorted[id] {
			remaining[id] = true
		}
	}
	for _, e := range edges {
		if remaining[e.From] && remaining[e.To] {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}

	// DFS to find a cycle.
	visited := make(map[string]bool)
	onStack := make(map[string]bool)
	parent := make(map[string]string)

	var dfs func(node string) []string
	dfs = func(node string) []string {
		visited[node] = true
		onStack[node] = true

		for _, next := range adj[node] {
			if !visited[next] {
				parent[next] = node
				if path := dfs(next); path != nil {
					return path
				}
			} else if onStack[next] {
				// Found cycle: next -> ... -> node -> next
				path := []string{next}
				cur := node
				for cur != next {
					path = append(path, cur)
					cur = parent[cur]
				}
				// Reverse to get cycle in order.
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				path = append(path, next) // close the cycle
				return path
			}
		}

		onStack[node] = false
		return nil
	}

	for node := range remaining {
		if !visited[node] {
			if path := dfs(node); path != nil {
				return path
			}
		}
	}

	return nil
}
