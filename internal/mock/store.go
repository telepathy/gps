package mock

import (
	"fmt"
	"gps/internal/model"
	"gps/internal/store"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ensure Store satisfies the store.Store interface.
var _ store.Store = (*Store)(nil)

// Store is a thread-safe in-memory data store
type Store struct {
	mu sync.RWMutex

	Silos []model.Silo
	Repos []model.Repo

	Plans   map[string]*model.ReleasePlan
	History []model.HistoryEntry

	// Users & RBAC (keyed by username / role name)
	Users       map[string]*model.User
	Roles       map[string]*model.Role
	userCounter int

	planCounter int
}

// NewStoreWithTree initializes the store with silo/repo data fetched from
// dalaran. Modules and dependency edges are NOT generated globally — they
// belong to individual plans and are created at plan creation/confirmation.
func NewStoreWithTree(silos []model.Silo, repos []model.Repo) *Store {
	return newStore(silos, repos)
}

func newStore(silos []model.Silo, repos []model.Repo) *Store {
	s := &Store{
		Silos: silos,
		Repos: repos,
		Plans: make(map[string]*model.ReleasePlan),
		Users: make(map[string]*model.User),
		Roles: make(map[string]*model.Role),
	}
	s.seedRoles()
	s.seedAdmin()
	return s
}

func (s *Store) seedRoles() {
	s.Roles[model.RoleAdmin] = &model.Role{
		Name:        model.RoleAdmin,
		Description: "管理员，拥有所有权限",
		Actions:     []string{model.ActionManage, model.ActionCreate, model.ActionRelease, model.ActionView},
	}
	s.Roles[model.RoleReleaser] = &model.Role{
		Name:        model.RoleReleaser,
		Description: "发布者，可在授权竖井内创建并执行发版",
		Actions:     []string{model.ActionCreate, model.ActionRelease, model.ActionView},
	}
	s.Roles[model.RoleViewer] = &model.Role{
		Name:        model.RoleViewer,
		Description: "观察者，仅查看",
		Actions:     []string{model.ActionView},
	}
}

func (s *Store) seedAdmin() {
	s.userCounter++
	s.Users["admin"] = &model.User{
		ID:           s.userCounter,
		Username:     "admin",
		Email:        "admin@gps.local",
		Roles:        []string{model.RoleAdmin},
		AllowedSilos: "*",
		CreatedAt:    time.Now(),
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

func (s *Store) GetAllRepos() []model.Repo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Repo, len(s.Repos))
	copy(out, s.Repos)
	sort.Slice(out, func(i, j int) bool {
		if out[i].SiloID != out[j].SiloID {
			return out[i].SiloID < out[j].SiloID
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (s *Store) FindRepoByPath(repositoryPath string) *model.Repo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	name := repositoryPath
	if idx := strings.LastIndex(repositoryPath, "/"); idx >= 0 {
		name = repositoryPath[idx+1:]
	}

	// Step 1: collect candidates by name.
	var candidates []model.Repo
	for _, r := range s.Repos {
		if r.Name == name {
			candidates = append(candidates, r)
		}
	}

	// Step 2: disambiguate.
	switch len(candidates) {
	case 0:
		return nil
	case 1:
		return &candidates[0]
	default:
		for _, r := range candidates {
			if model.UrlMatchesPath(r.URL, repositoryPath) {
				return &r
			}
		}
		return nil
	}
}

func (s *Store) UpdateRepoBranch(repoID, branch string) (*model.Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Repos {
		if s.Repos[i].ID == repoID {
			s.Repos[i].ReleaseBranch = branch
			r := s.Repos[i]
			return &r, nil
		}
	}
	return nil, fmt.Errorf("repo not found")
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

	// Gather repos for selected silos.
	siloSet := make(map[string]bool)
	for _, id := range req.SiloIDs {
		siloSet[id] = true
	}
	var planRepos []model.Repo
	for _, r := range s.Repos {
		if siloSet[r.SiloID] {
			planRepos = append(planRepos, r)
		}
	}

	// Generate modules for this plan (GA-identified, with pending-external).
	modules := GenerateModulesForPlan(planRepos, int64(s.planCounter*1000+42))

	// Build plan entries.
	repoTargetVersions := make(map[string]string)
	var entries []model.PlanModuleEntry
	for _, m := range modules {
		repo := s.findRepo(m.RepoID)
		silo := s.findSilo(m.SiloID)
		repoName, siloName := "", ""
		if repo != nil {
			repoName = repo.Name
		}
		if silo != nil {
			siloName = silo.Name
		}

		// Compute target version (only for internal modules).
		targetVersion := m.CurrentVersion
		if m.Kind == model.KindInternal {
			if _, ok := repoTargetVersions[m.RepoID]; !ok {
				repoTargetVersions[m.RepoID] = bumpPatch(m.CurrentVersion)
			}
			targetVersion = repoTargetVersions[m.RepoID]
		}

		entries = append(entries, model.PlanModuleEntry{
			ModuleID:      m.ID,
			ModuleName:    m.Name,
			Kind:          m.Kind,
			Group:         m.Group,
			Artifact:      m.Artifact,
			GradlePath:    m.GradlePath,
			RepoID:        m.RepoID,
			RepoName:      repoName,
			SiloID:        m.SiloID,
			SiloName:      siloName,
			PrevVersion:   m.CurrentVersion,
			TargetVersion: targetVersion,
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
	for i := range plan.Modules {
		if plan.Modules[i].Kind != model.KindInternal {
			continue
		}
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

	// Build module list and edges.
	var modules []model.Module
	for _, pe := range plan.Modules {
		modules = append(modules, model.Module{
			ID:             pe.ModuleID,
			Group:          pe.Group,
			Artifact:       pe.Artifact,
			GradlePath:     pe.GradlePath,
			RepoID:         pe.RepoID,
			SiloID:         pe.SiloID,
			Name:           pe.ModuleName,
			Kind:           pe.Kind,
			CurrentVersion: pe.PrevVersion,
		})
	}
	edges := GenerateEdges(modules)

	var moduleIDs []string
	for _, m := range modules {
		moduleIDs = append(moduleIDs, m.ID)
	}
	sorted, hasCycle, cyclePath := model.TopologicalSort(moduleIDs, edges)
	if hasCycle {
		return fmt.Errorf("cycle detected: %v", cyclePath)
	}

	plan.Status = model.PlanConfirmed
	plan.DepGraph = &model.DependencyGraph{
		Nodes:       moduleIDs,
		Edges:       edges,
		SortedOrder: sorted,
	}

	// Pre-set pending-external modules to SUCCESS (they are confirmed at gate time).
	for i := range plan.Modules {
		if plan.Modules[i].Kind == model.KindPendingExternal {
			plan.Modules[i].Status = model.StatusSuccess
		}
	}

	return nil
}

func (s *Store) ConfirmPendingExternal(planID string, gas []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := s.Plans[planID]
	if !ok {
		return fmt.Errorf("plan not found")
	}
	if plan.Status != model.PlanConfirmed {
		return fmt.Errorf("plan must be CONFIRMED to confirm external modules")
	}

	gaSet := make(map[string]bool)
	for _, ga := range gas {
		gaSet[ga] = true
	}

	for i := range plan.Modules {
		if plan.Modules[i].Kind == model.KindPendingExternal && gaSet[plan.Modules[i].ModuleID] {
			plan.Modules[i].Status = model.StatusSuccess
		}
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

		var duration string
		if plan.StartedAt != nil {
			d := now.Sub(*plan.StartedAt)
			duration = fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
		}

		entry := model.HistoryEntry{
			PlanID:       planID,
			SiloIDs:      plan.SiloIDs,
			SiloNames:    siloNames,
			Status:       status,
			TotalModules: len(plan.Modules),
			Succeeded:    succeeded,
			Failed:       failed,
			Skipped:      skipped,
			Duration:     duration,
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
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// --- Users & RBAC ---

func (s *Store) FindOrCreateUser(in *model.User) (*model.User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.Users[in.Username]; ok {
		if in.GitlabID != 0 {
			existing.GitlabID = in.GitlabID
		}
		if in.Name != "" {
			existing.Name = in.Name
		}
		if in.Email != "" {
			existing.Email = in.Email
		}
		if in.AvatarURL != "" {
			existing.AvatarURL = in.AvatarURL
		}
		return existing, false, nil
	}
	s.userCounter++
	u := &model.User{
		ID:           s.userCounter,
		Username:     in.Username,
		Name:         in.Name,
		Email:        in.Email,
		AvatarURL:    in.AvatarURL,
		GitlabID:     in.GitlabID,
		Roles:        []string{model.RoleViewer},
		AllowedSilos: "",
		CreatedAt:    time.Now(),
	}
	s.Users[u.Username] = u
	return u, true, nil
}

func (s *Store) GetUserByUsername(username string) *model.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Users[username]
}

func (s *Store) GetUserByID(id int) *model.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.Users {
		if u.ID == id {
			return u
		}
	}
	return nil
}

func (s *Store) ListUsers() []*model.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.User, 0, len(s.Users))
	for _, u := range s.Users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) CountUsers() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Users)
}

func (s *Store) ImportUser(entry model.ImportUserEntry) (bool, error) {
	username := strings.TrimSpace(entry.Username)
	if username == "" {
		return false, fmt.Errorf("empty username")
	}
	roles := entry.Roles
	if len(roles) == 0 {
		roles = []string{model.RoleViewer}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.Users[username]; exists {
		return false, nil
	}
	for _, r := range roles {
		if _, ok := s.Roles[r]; !ok {
			return false, fmt.Errorf("unknown role: %s", r)
		}
	}
	s.userCounter++
	s.Users[username] = &model.User{
		ID:           s.userCounter,
		Username:     username,
		Email:        strings.TrimSpace(entry.Email),
		Roles:        roles,
		AllowedSilos: strings.TrimSpace(entry.AllowedSilos),
		CreatedAt:    time.Now(),
	}
	return true, nil
}

func (s *Store) GetRoles() []*model.Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Role, 0, len(s.Roles))
	for _, r := range s.Roles {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) SetUserRoles(userID int, roles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var u *model.User
	for _, candidate := range s.Users {
		if candidate.ID == userID {
			u = candidate
			break
		}
	}
	if u == nil {
		return fmt.Errorf("user not found")
	}
	for _, r := range roles {
		if _, ok := s.Roles[r]; !ok {
			return fmt.Errorf("unknown role: %s", r)
		}
	}
	u.Roles = roles
	return nil
}

func (s *Store) SetUserAllowedSilos(userID int, allowedSilos string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.Users {
		if u.ID == userID {
			u.AllowedSilos = allowedSilos
			return nil
		}
	}
	return fmt.Errorf("user not found")
}

func (s *Store) UserActions(u *model.User) map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	actions := make(map[string]bool)
	for _, rn := range u.Roles {
		if role, ok := s.Roles[rn]; ok {
			for _, a := range role.Actions {
				actions[a] = true
			}
		}
	}
	return actions
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

func (s *Store) nextRepoIDNum() int {
	maxID := 0
	for _, r := range s.Repos {
		if strings.HasPrefix(r.ID, "repo-") {
			if n, err := strconv.Atoi(strings.TrimPrefix(r.ID, "repo-")); err == nil && n > maxID {
				maxID = n
			}
		}
	}
	return maxID + 1
}

func (s *Store) SyncProductTree(dalaranSilos []model.Silo, dalaranRepos []model.Repo) (*model.SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := &model.SyncResult{}

	dalaranSiloMap := make(map[string]bool)
	for _, dsi := range dalaranSilos {
		dalaranSiloMap[dsi.ID] = true
	}

	for _, si := range s.Silos {
		if !dalaranSiloMap[si.ID] {
			result.SilosDeleted++
		}
	}

	seenSilo := make(map[string]bool)
	var filteredSilos []model.Silo
	for _, si := range s.Silos {
		if dalaranSiloMap[si.ID] && !seenSilo[si.ID] {
			filteredSilos = append(filteredSilos, si)
			seenSilo[si.ID] = true
		}
	}
	for _, dsi := range dalaranSilos {
		if !seenSilo[dsi.ID] {
			filteredSilos = append(filteredSilos, dsi)
			seenSilo[dsi.ID] = true
		}
	}
	s.Silos = filteredSilos

	repoURLMap := make(map[string]bool)
	for _, r := range s.Repos {
		repoURLMap[r.URL] = true
	}

	var newRepos []model.Repo
	nextID := s.nextRepoIDNum()
	for _, dri := range dalaranRepos {
		if !repoURLMap[dri.URL] {
			newRepos = append(newRepos, model.Repo{
				ID:            fmt.Sprintf("repo-%04d", nextID),
				SiloID:        dri.SiloID,
				Name:          dri.Name,
				URL:           dri.URL,
				ReleaseBranch: "main",
			})
			nextID++
			result.ReposAdded++
		}
	}
	s.Repos = append(s.Repos, newRepos...)

	dalaranRepoURLs := make(map[string]bool)
	for _, dri := range dalaranRepos {
		dalaranRepoURLs[dri.URL] = true
	}

	var keptRepos []model.Repo
	for _, r := range s.Repos {
		if dalaranRepoURLs[r.URL] {
			keptRepos = append(keptRepos, r)
		} else {
			result.ReposDeleted++
		}
	}
	s.Repos = keptRepos

	return result, nil
}
