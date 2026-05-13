package mock

import (
	"fmt"
	"gps/internal/model"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Store is a thread-safe in-memory data store
type Store struct {
	mu sync.RWMutex

	Silos   []model.Silo
	Repos   []model.Repo
	Modules []model.Module
	Edges   []model.DepEdge

	Plans   map[string]*model.ReleasePlan
	History []model.HistoryEntry

	planCounter int

	// SSE subscribers per plan
	sseSubscribers map[string]map[chan model.SSEEvent]bool
	sseMu          sync.RWMutex
}

// NewStore initializes the store with generated mock data
func NewStore() *Store {
	silos, repos, modules, edges := GenerateData()

	s := &Store{
		Silos:          silos,
		Repos:          repos,
		Modules:        modules,
		Edges:          edges,
		Plans:          make(map[string]*model.ReleasePlan),
		sseSubscribers: make(map[string]map[chan model.SSEEvent]bool),
	}

	// Pre-seed 3 historical releases
	s.seedHistory()

	return s
}

func (s *Store) seedHistory() {
	now := time.Now()

	// Seed 3 historical releases as full plans
	configs := []struct {
		numSilos  int
		failed    int
		skipped   int
		status    model.PlanStatus
		hoursAgo  int
		durMin    int
	}{
		{3, 0, 0, model.PlanCompleted, 72, 5},
		{5, 3, 5, model.PlanAborted, 48, 7},
		{7, 2, 4, model.PlanCompleted, 24, 9},
	}

	for i, cfg := range configs {
		planID := fmt.Sprintf("hist-%03d", i+1)
		createdAt := now.Add(-time.Duration(cfg.hoursAgo) * time.Hour)
		startedAt := createdAt.Add(10 * time.Second)
		completedAt := createdAt.Add(time.Duration(cfg.durMin) * time.Minute)

		siloIDs := []string{}
		siloNames := []string{}
		for j := 0; j < cfg.numSilos && j < len(s.Silos); j++ {
			siloIDs = append(siloIDs, s.Silos[j].ID)
			siloNames = append(siloNames, s.Silos[j].Name)
		}

		// Gather modules for these silos
		siloSet := make(map[string]bool)
		for _, id := range siloIDs {
			siloSet[id] = true
		}
		var entries []model.PlanModuleEntry
		for _, m := range s.Modules {
			if !siloSet[m.SiloID] {
				continue
			}
			repo := s.findRepo(m.RepoID)
			silo := s.findSilo(m.SiloID)
			repoName, siloName := "", ""
			if repo != nil {
				repoName = repo.Name
			}
			if silo != nil {
				siloName = silo.Name
			}
			entries = append(entries, model.PlanModuleEntry{
				ModuleID:      m.ID,
				ModuleName:    m.Name,
				RepoID:        m.RepoID,
				RepoName:      repoName,
				SiloID:        m.SiloID,
				SiloName:      siloName,
				PrevVersion:   m.CurrentVersion,
				TargetVersion: bumpPatch(m.CurrentVersion),
				Status:        model.StatusSuccess,
			})
		}

		// Assign terminal statuses
		failIdx := 0
		skipIdx := 0
		for j := len(entries) - 1; j >= 0; j-- {
			if failIdx < cfg.failed {
				entries[j].Status = model.StatusFailed
				entries[j].ErrorMsg = "Build error: compilation failed in unit tests"
				failIdx++
			} else if skipIdx < cfg.skipped {
				entries[j].Status = model.StatusSkipped
				entries[j].ErrorMsg = "upstream dependency failed"
				skipIdx++
			}
		}

		succeeded := len(entries) - cfg.failed - cfg.skipped

		// Compute dep graph for the plan
		var moduleIDs []string
		for _, e := range entries {
			moduleIDs = append(moduleIDs, e.ModuleID)
		}
		edges := s.getEdgesForModulesLocked(moduleIDs)
		sorted := TopologicalSort(moduleIDs, edges)

		plan := &model.ReleasePlan{
			ID:              planID,
			SiloIDs:         siloIDs,
			DmsBranch:       "release/2025Q2",
			Concurrency:     4,
			FailureStrategy: model.StrategyAbort,
			MaxRetries:      3,
			Status:          cfg.status,
			Phase:           model.PhaseCompleted,
			Modules:         entries,
			DepGraph: &model.DependencyGraph{
				Nodes:       moduleIDs,
				Edges:       edges,
				SortedOrder: sorted,
			},
			CreatedAt:   createdAt,
			StartedAt:   &startedAt,
			CompletedAt: &completedAt,
		}
		s.Plans[planID] = plan

		s.History = append(s.History, model.HistoryEntry{
			PlanID:       planID,
			SiloIDs:      siloIDs,
			SiloNames:    siloNames,
			Status:       cfg.status,
			TotalModules: len(entries),
			Succeeded:    succeeded,
			Failed:       cfg.failed,
			Skipped:      cfg.skipped,
			Duration:     fmt.Sprintf("%dm%ds", cfg.durMin, 30+i*10),
			CreatedAt:    createdAt,
			CompletedAt:  &completedAt,
		})
	}
}

// --- Product Tree queries ---

func (s *Store) GetSilos() []model.Silo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Silos
}

func (s *Store) GetReposBySilo(siloID string) []model.Repo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []model.Repo
	for _, r := range s.Repos {
		if r.SiloID == siloID {
			result = append(result, r)
		}
	}
	return result
}

func (s *Store) GetModulesByRepo(repoID string) []model.Module {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []model.Module
	for _, m := range s.Modules {
		if m.RepoID == repoID {
			result = append(result, m)
		}
	}
	return result
}

func (s *Store) GetModulesBySilos(siloIDs []string) []model.Module {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idSet := make(map[string]bool)
	for _, id := range siloIDs {
		idSet[id] = true
	}
	var result []model.Module
	for _, m := range s.Modules {
		if idSet[m.SiloID] {
			result = append(result, m)
		}
	}
	return result
}

func (s *Store) GetEdgesForModules(moduleIDs []string) []model.DepEdge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idSet := make(map[string]bool)
	for _, id := range moduleIDs {
		idSet[id] = true
	}
	var result []model.DepEdge
	for _, e := range s.Edges {
		if idSet[e.From] && idSet[e.To] {
			result = append(result, e)
		}
	}
	return result
}

// --- Lookup helpers ---

func (s *Store) GetModule(id string) *model.Module {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.Modules {
		if m.ID == id {
			return &m
		}
	}
	return nil
}

func (s *Store) GetRepo(id string) *model.Repo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.Repos {
		if r.ID == id {
			return &r
		}
	}
	return nil
}

func (s *Store) GetSilo(id string) *model.Silo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, si := range s.Silos {
		if si.ID == id {
			return &si
		}
	}
	return nil
}

// --- Plan operations ---

func (s *Store) CreatePlan(req model.CreatePlanRequest) *model.ReleasePlan {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.planCounter++
	planID := fmt.Sprintf("plan-%03d", s.planCounter)

	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	strategy := req.FailureStrategy
	if strategy == "" {
		strategy = model.StrategyAbort
	}
	maxRetries := req.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	// Gather modules for selected silos
	siloSet := make(map[string]bool)
	for _, id := range req.SiloIDs {
		siloSet[id] = true
	}

	// Compute repo-level target versions (all modules in same repo share version)
	repoTargetVersions := make(map[string]string)

	var entries []model.PlanModuleEntry
	for _, m := range s.Modules {
		if !siloSet[m.SiloID] {
			continue
		}
		repo := s.findRepo(m.RepoID)
		silo := s.findSilo(m.SiloID)
		repoName := ""
		siloName := ""
		if repo != nil {
			repoName = repo.Name
		}
		if silo != nil {
			siloName = silo.Name
		}

		// Compute target version once per repo
		if _, ok := repoTargetVersions[m.RepoID]; !ok {
			repoTargetVersions[m.RepoID] = bumpPatch(m.CurrentVersion)
		}

		entries = append(entries, model.PlanModuleEntry{
			ModuleID:      m.ID,
			ModuleName:    m.Name,
			RepoID:        m.RepoID,
			RepoName:      repoName,
			SiloID:        m.SiloID,
			SiloName:      siloName,
			PrevVersion:   m.CurrentVersion,
			TargetVersion: repoTargetVersions[m.RepoID],
			Status:        model.StatusPending,
		})
	}

	plan := &model.ReleasePlan{
		ID:              planID,
		SiloIDs:         req.SiloIDs,
		DmsBranch:       req.DmsBranch,
		Concurrency:     concurrency,
		FailureStrategy: strategy,
		MaxRetries:      maxRetries,
		Status:          model.PlanDraft,
		Phase:           model.PhaseNone,
		Modules:         entries,
		CreatedAt:       time.Now(),
	}

	s.Plans[planID] = plan
	return plan
}

func (s *Store) GetPlan(id string) *model.ReleasePlan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Plans[id]
}

func (s *Store) GetPlans() []*model.ReleasePlan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*model.ReleasePlan
	for _, p := range s.Plans {
		result = append(result, p)
	}
	return result
}

func (s *Store) UpdateVersions(planID string, versions map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := s.Plans[planID]
	if !ok {
		return fmt.Errorf("plan not found")
	}
	if plan.Status != model.PlanDraft {
		return fmt.Errorf("plan is not in DRAFT status")
	}
	// versions is keyed by repo_id -> new version
	// Apply to all modules in that repo
	for i := range plan.Modules {
		if v, exists := versions[plan.Modules[i].RepoID]; exists {
			plan.Modules[i].TargetVersion = v
			plan.Modules[i].IsOverridden = true
		}
	}
	return nil
}

func (s *Store) ConfirmPlan(planID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := s.Plans[planID]
	if !ok {
		return fmt.Errorf("plan not found")
	}
	if plan.Status != model.PlanDraft {
		return fmt.Errorf("plan is not in DRAFT status")
	}
	plan.Status = model.PlanConfirmed

	// Compute dependency graph
	var moduleIDs []string
	for _, m := range plan.Modules {
		moduleIDs = append(moduleIDs, m.ModuleID)
	}
	edges := s.getEdgesForModulesLocked(moduleIDs)
	sorted := TopologicalSort(moduleIDs, edges)
	plan.DepGraph = &model.DependencyGraph{
		Nodes:       moduleIDs,
		Edges:       edges,
		SortedOrder: sorted,
	}

	return nil
}

func (s *Store) SetPlanRunning(planID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan, ok := s.Plans[planID]; ok {
		plan.Status = model.PlanRunning
		now := time.Now()
		plan.StartedAt = &now
	}
}

func (s *Store) SetPlanPhase(planID string, phase model.ReleasePhase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan, ok := s.Plans[planID]; ok {
		plan.Phase = phase
	}
}

func (s *Store) SetModuleStatus(planID, moduleID string, status model.ModuleStatus, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan, ok := s.Plans[planID]; ok {
		for i := range plan.Modules {
			if plan.Modules[i].ModuleID == moduleID {
				plan.Modules[i].Status = status
				if errMsg != "" {
					plan.Modules[i].ErrorMsg = errMsg
				}
				now := time.Now()
				if status == model.StatusReleasing {
					plan.Modules[i].StartTime = &now
				}
				if status == model.StatusSuccess || status == model.StatusFailed || status == model.StatusSkipped {
					plan.Modules[i].EndTime = &now
				}
				if status == model.StatusRetrying {
					plan.Modules[i].RetryCount++
				}
				break
			}
		}
	}
}

func (s *Store) CompletePlan(planID string, status model.PlanStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan, ok := s.Plans[planID]; ok {
		plan.Status = status
		plan.Phase = model.PhaseCompleted
		now := time.Now()
		plan.CompletedAt = &now

		// Add to history
		succeeded, failed, skipped := 0, 0, 0
		for _, m := range plan.Modules {
			switch m.Status {
			case model.StatusSuccess:
				succeeded++
			case model.StatusFailed:
				failed++
			case model.StatusSkipped:
				skipped++
			}
		}

		siloNames := []string{}
		for _, sid := range plan.SiloIDs {
			if silo := s.findSilo(sid); silo != nil {
				siloNames = append(siloNames, silo.Name)
			}
		}

		duration := now.Sub(*plan.StartedAt)
		entry := model.HistoryEntry{
			PlanID:       planID,
			SiloIDs:      plan.SiloIDs,
			SiloNames:    siloNames,
			Status:       status,
			TotalModules: len(plan.Modules),
			Succeeded:    succeeded,
			Failed:       failed,
			Skipped:      skipped,
			Duration:     fmt.Sprintf("%dm%ds", int(duration.Minutes()), int(duration.Seconds())%60),
			CreatedAt:    plan.CreatedAt,
			CompletedAt:  &now,
		}
		s.History = append(s.History, entry)
	}
}

func (s *Store) GetProgress(planID string) *model.ReleaseProgress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, ok := s.Plans[planID]
	if !ok {
		return nil
	}
	p := &model.ReleaseProgress{
		PlanID:       planID,
		Phase:        plan.Phase,
		Status:       plan.Status,
		TotalModules: len(plan.Modules),
		Modules:      plan.Modules,
	}
	for _, m := range plan.Modules {
		switch m.Status {
		case model.StatusPending:
			p.Pending++
		case model.StatusTagged:
			p.Tagged++
		case model.StatusReleasing, model.StatusRetrying:
			p.Releasing++
		case model.StatusSuccess:
			p.Succeeded++
		case model.StatusFailed:
			p.Failed++
		case model.StatusSkipped:
			p.Skipped++
		}
	}
	return p
}

func (s *Store) GetHistory() []model.HistoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]model.HistoryEntry, len(s.History))
	copy(result, s.History)
	// Reverse to show newest first
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// --- SSE ---

func (s *Store) Subscribe(planID string) chan model.SSEEvent {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	ch := make(chan model.SSEEvent, 100)
	if s.sseSubscribers[planID] == nil {
		s.sseSubscribers[planID] = make(map[chan model.SSEEvent]bool)
	}
	s.sseSubscribers[planID][ch] = true
	return ch
}

func (s *Store) Unsubscribe(planID string, ch chan model.SSEEvent) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	if subs, ok := s.sseSubscribers[planID]; ok {
		delete(subs, ch)
		close(ch)
	}
}

func (s *Store) Broadcast(planID string, event model.SSEEvent) {
	s.sseMu.RLock()
	defer s.sseMu.RUnlock()
	if subs, ok := s.sseSubscribers[planID]; ok {
		for ch := range subs {
			select {
			case ch <- event:
			default:
				// Drop if buffer full
			}
		}
	}
}

// --- Helpers ---

func (s *Store) findRepo(id string) *model.Repo {
	for _, r := range s.Repos {
		if r.ID == id {
			return &r
		}
	}
	return nil
}

func (s *Store) findSilo(id string) *model.Silo {
	for _, si := range s.Silos {
		if si.ID == id {
			return &si
		}
	}
	return nil
}

func (s *Store) getEdgesForModulesLocked(moduleIDs []string) []model.DepEdge {
	idSet := make(map[string]bool)
	for _, id := range moduleIDs {
		idSet[id] = true
	}
	var result []model.DepEdge
	for _, e := range s.Edges {
		if idSet[e.From] && idSet[e.To] {
			result = append(result, e)
		}
	}
	return result
}

func bumpPatch(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return version
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return version
	}
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
}
