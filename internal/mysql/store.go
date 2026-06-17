package mysql

import (
	"fmt"
	"gps/internal/mock"
	"gps/internal/model"
	"log"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store implements store.Store backed by MySQL via GORM.
type Store struct {
	db *gorm.DB
}

// NewStore creates a MySQL-backed store. It auto-migrates the schema, seeds
// built-in roles and admin user, then persists the product tree (silos, repos)
// fetched from dalaran.
func NewStore(db *gorm.DB, silos []model.Silo, repos []model.Repo) *Store {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		log.Fatalf("mysql: auto-migrate failed: %v", err)
	}
	log.Println("mysql: schema migrated")
	s.seedRoles()
	s.seedAdmin()
	s.seedTree(silos, repos)
	return s
}

func (s *Store) migrate() error {
	return s.db.AutoMigrate(
		&model.GPSSilo{},
		&model.GPSRepo{},
		&model.GPSReleasePlan{},
		&model.GPSPlanModule{},
		&model.GPSPlanDepEdge{},
		&model.GPSPlanTopoOrder{},
		&model.GPSPlanGradleSubproject{},
		&model.GPSReleaseHistory{},
		&model.GPSUser{},
		&model.GPSRole{},
		&model.GPSUserRole{},
	)
}

func (s *Store) seedRoles() {
	roles := []model.GPSRole{
		{Name: model.RoleAdmin, Description: "管理员，拥有所有权限", Actions: datatypes.NewJSONType([]string{model.ActionManage, model.ActionCreate, model.ActionRelease, model.ActionView})},
		{Name: model.RoleReleaser, Description: "发布者，可在授权竖井内创建并执行发版", Actions: datatypes.NewJSONType([]string{model.ActionCreate, model.ActionRelease, model.ActionView})},
		{Name: model.RoleViewer, Description: "观察者，仅查看", Actions: datatypes.NewJSONType([]string{model.ActionView})},
	}
	for _, r := range roles {
		s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&r)
	}
}

func (s *Store) seedAdmin() {
	var count int64
	s.db.Model(&model.GPSUser{}).Where("username = ?", "admin").Count(&count)
	if count > 0 {
		return
	}
	s.db.Create(&model.GPSUser{
		Username:     "admin",
		Email:        "admin@gps.local",
		Roles:        datatypes.NewJSONType([]string{model.RoleAdmin}),
		AllowedSilos: "*",
		CreatedAt:    time.Now(),
	})
}

func (s *Store) seedTree(silos []model.Silo, repos []model.Repo) {
	var siloCount int64
	s.db.Model(&model.GPSSilo{}).Count(&siloCount)
	if siloCount > 0 {
		return
	}
	for _, si := range silos {
		s.db.Create(&model.GPSSilo{ID: si.ID, Name: si.Name, Desc: si.Desc})
	}
	for _, r := range repos {
		s.db.Create(&model.GPSRepo{ID: r.ID, SiloID: r.SiloID, Name: r.Name, URL: r.URL, ReleaseBranch: r.ReleaseBranch})
	}
	log.Printf("mysql: seeded %d silos, %d repos", len(silos), len(repos))
}

// --- Product tree ---

func (s *Store) GetSilos() []model.Silo {
	var rows []model.GPSSilo
	s.db.Find(&rows)
	out := make([]model.Silo, len(rows))
	for i, r := range rows {
		out[i] = model.Silo{ID: r.ID, Name: r.Name, Desc: r.Desc}
	}
	return out
}

func (s *Store) GetReposBySilo(siloID string) []model.Repo {
	var rows []model.GPSRepo
	s.db.Where("silo_id = ?", siloID).Find(&rows)
	return toRepos(rows)
}

func (s *Store) GetRepo(id string) *model.Repo {
	var row model.GPSRepo
	if err := s.db.First(&row, "id = ?", id).Error; err != nil {
		return nil
	}
	r := toRepo(row)
	return &r
}

func (s *Store) GetSilo(id string) *model.Silo {
	var row model.GPSSilo
	if err := s.db.First(&row, "id = ?", id).Error; err != nil {
		return nil
	}
	return &model.Silo{ID: row.ID, Name: row.Name, Desc: row.Desc}
}

func (s *Store) GetAllRepos() []model.Repo {
	var rows []model.GPSRepo
	s.db.Order("silo_id, name").Find(&rows)
	return toRepos(rows)
}

func (s *Store) UpdateRepoBranch(repoID, branch string) (*model.Repo, error) {
	result := s.db.Model(&model.GPSRepo{}).Where("id = ?", repoID).Update("release_branch", branch)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("repo not found")
	}
	return s.GetRepo(repoID), nil
}

// --- Plans ---

func (s *Store) CreatePlan(req model.CreatePlanRequest) *model.ReleasePlan {
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

	planID := s.nextPlanID()

	// Gather repos for selected silos.
	var repoRows []model.GPSRepo
	s.db.Where("silo_id IN ?", req.SiloIDs).Find(&repoRows)
	var planRepos []model.Repo
	for _, r := range repoRows {
		planRepos = append(planRepos, toRepo(r))
	}

	// Generate GA-identified modules.
	modules := mock.GenerateModulesForPlan(planRepos, int64(len(planRepos)*1000+42))

	repoTargetVersions := make(map[string]string)
	var entries []model.PlanModuleEntry
	for _, m := range modules {
		repo := s.GetRepo(m.RepoID)
		silo := s.GetSilo(m.SiloID)
		repoName, siloName := "", ""
		if repo != nil {
			repoName = repo.Name
		}
		if silo != nil {
			siloName = silo.Name
		}

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

	s.db.Transaction(func(tx *gorm.DB) error {
		tx.Create(&model.GPSReleasePlan{
			ID:              planID,
			SiloIDs:         datatypes.NewJSONType(req.SiloIDs),
			DmsBranch:       req.DmsBranch,
			Concurrency:     concurrency,
			FailureStrategy: string(strategy),
			MaxRetries:      maxRetries,
			Status:          string(model.PlanDraft),
			Phase:           string(model.PhaseNone),
		})
		for _, e := range entries {
			tx.Create(&model.GPSPlanModule{
				PlanID:        planID,
				GA:            e.ModuleID,
				ModuleName:    e.ModuleName,
				Kind:          e.Kind,
				GroupID:       e.Group,
				Artifact:      e.Artifact,
				GradlePath:    e.GradlePath,
				RepoID:        e.RepoID,
				RepoName:      e.RepoName,
				SiloID:        e.SiloID,
				SiloName:      e.SiloName,
				PrevVersion:   e.PrevVersion,
				TargetVersion: e.TargetVersion,
				Status:        string(model.StatusPending),
			})
		}
		return nil
	})

	return &model.ReleasePlan{
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
}

func (s *Store) GetPlan(id string) *model.ReleasePlan {
	var plan model.GPSReleasePlan
	if err := s.db.First(&plan, "id = ?", id).Error; err != nil {
		return nil
	}

	var pmRows []model.GPSPlanModule
	s.db.Where("plan_id = ?", id).Order("ga").Find(&pmRows)

	entries := make([]model.PlanModuleEntry, len(pmRows))
	for i, pm := range pmRows {
		entries[i] = model.PlanModuleEntry{
			ModuleID:      pm.GA,
			ModuleName:    pm.ModuleName,
			Kind:          pm.Kind,
			Group:         pm.GroupID,
			Artifact:      pm.Artifact,
			GradlePath:    pm.GradlePath,
			RepoID:        pm.RepoID,
			RepoName:      pm.RepoName,
			SiloID:        pm.SiloID,
			SiloName:      pm.SiloName,
			PrevVersion:   pm.PrevVersion,
			TargetVersion: pm.TargetVersion,
			IsOverridden:  pm.IsOverridden,
			Status:        model.ModuleStatus(pm.Status),
			PipelineID:    pm.PipelineID,
			StartTime:     pm.StartTime,
			EndTime:       pm.EndTime,
			ErrorMsg:      pm.ErrorMsg,
			RetryCount:    pm.RetryCount,
		}
	}

	// Load dep graph if confirmed.
	var depGraph *model.DependencyGraph
	var edgeRows []model.GPSPlanDepEdge
	s.db.Where("plan_id = ?", id).Find(&edgeRows)
	if len(edgeRows) > 0 {
		var topoRows []model.GPSPlanTopoOrder
		s.db.Where("plan_id = ?", id).Order("seq").Find(&topoRows)

		edges := make([]model.DepEdge, len(edgeRows))
		for i, e := range edgeRows {
			edges[i] = model.DepEdge{From: e.FromGA, To: e.ToGA, CrossRepo: e.CrossRepo}
		}
		nodes := make([]string, len(entries))
		for i, e := range entries {
			nodes[i] = e.ModuleID
		}
		sorted := make([]string, len(topoRows))
		for i, t := range topoRows {
			sorted[i] = t.GA
		}
		depGraph = &model.DependencyGraph{Nodes: nodes, Edges: edges, SortedOrder: sorted}
	}

	siloIDs := plan.SiloIDs.Data()

	return &model.ReleasePlan{
		ID:              plan.ID,
		SiloIDs:         siloIDs,
		DmsBranch:       plan.DmsBranch,
		Concurrency:     plan.Concurrency,
		FailureStrategy: model.FailureStrategy(plan.FailureStrategy),
		MaxRetries:      plan.MaxRetries,
		Status:          model.PlanStatus(plan.Status),
		Phase:           model.ReleasePhase(plan.Phase),
		Modules:         entries,
		DepGraph:        depGraph,
		CreatedAt:       plan.CreatedAt,
		StartedAt:       plan.StartedAt,
		CompletedAt:     plan.CompletedAt,
	}
}

func (s *Store) GetPlans() []*model.ReleasePlan {
	var rows []model.GPSReleasePlan
	s.db.Find(&rows)
	out := make([]*model.ReleasePlan, len(rows))
	for i, r := range rows {
		out[i] = s.GetPlan(r.ID)
	}
	return out
}

func (s *Store) UpdateVersions(planID string, versions map[string]string) error {
	var plan model.GPSReleasePlan
	if err := s.db.First(&plan, "id = ?", planID).Error; err != nil {
		return fmt.Errorf("plan not found")
	}
	if plan.Status != string(model.PlanDraft) {
		return fmt.Errorf("plan is not in DRAFT status")
	}
	var modules []model.GPSPlanModule
	s.db.Where("plan_id = ? AND kind = ?", planID, model.KindInternal).Find(&modules)
	for _, pm := range modules {
		if v, ok := versions[pm.RepoID]; ok {
			s.db.Model(&pm).Updates(map[string]interface{}{"target_version": v, "is_overridden": true})
		}
	}
	return nil
}

func (s *Store) ConfirmPlan(planID string) error {
	var plan model.GPSReleasePlan
	if err := s.db.First(&plan, "id = ?", planID).Error; err != nil {
		return fmt.Errorf("plan not found")
	}
	if plan.Status != string(model.PlanDraft) {
		return fmt.Errorf("plan is not in DRAFT status")
	}

	var pmRows []model.GPSPlanModule
	s.db.Where("plan_id = ?", planID).Find(&pmRows)
	var modules []model.Module
	moduleIDs := make([]string, len(pmRows))
	for i, pm := range pmRows {
		modules = append(modules, model.Module{
			ID: pm.GA, Group: pm.GroupID, Artifact: pm.Artifact,
			GradlePath: pm.GradlePath, RepoID: pm.RepoID, SiloID: pm.SiloID,
			Name: pm.ModuleName, Kind: pm.Kind, CurrentVersion: pm.PrevVersion,
		})
		moduleIDs[i] = pm.GA
	}

	edges := mock.GenerateEdges(modules)
	sorted, hasCycle, cyclePath := model.TopologicalSort(moduleIDs, edges)
	if hasCycle {
		return fmt.Errorf("cycle detected: %v", cyclePath)
	}

	s.db.Transaction(func(tx *gorm.DB) error {
		for _, e := range edges {
			tx.Create(&model.GPSPlanDepEdge{PlanID: planID, FromGA: e.From, ToGA: e.To, CrossRepo: e.CrossRepo})
		}
		for i, ga := range sorted {
			tx.Create(&model.GPSPlanTopoOrder{PlanID: planID, Seq: i, GA: ga})
		}
		// Save gradle subproject mappings for audit.
		for _, pm := range pmRows {
			if pm.GradlePath != "" {
				tx.Create(&model.GPSPlanGradleSubproject{PlanID: planID, RepoID: pm.RepoID, GradlePath: pm.GradlePath, GA: pm.GA})
			}
		}
		tx.Model(&plan).Update("status", string(model.PlanConfirmed))
		// Pre-set pending-external to SUCCESS.
		tx.Model(&model.GPSPlanModule{}).
			Where("plan_id = ? AND kind = ?", planID, model.KindPendingExternal).
			Update("status", string(model.StatusSuccess))
		return nil
	})
	return nil
}

func (s *Store) ConfirmPendingExternal(planID string, gas []string) error {
	var plan model.GPSReleasePlan
	if err := s.db.First(&plan, "id = ?", planID).Error; err != nil {
		return fmt.Errorf("plan not found")
	}
	if plan.Status != string(model.PlanConfirmed) {
		return fmt.Errorf("plan must be CONFIRMED to confirm external modules")
	}
	s.db.Model(&model.GPSPlanModule{}).
		Where("plan_id = ? AND ga IN ? AND kind = ?", planID, gas, model.KindPendingExternal).
		Update("status", string(model.StatusSuccess))
	return nil
}

func (s *Store) SetPlanRunning(planID string) {
	now := time.Now()
	s.db.Model(&model.GPSReleasePlan{}).Where("id = ?", planID).Updates(map[string]interface{}{
		"status": string(model.PlanRunning), "started_at": &now,
	})
}

func (s *Store) SetPlanPhase(planID string, phase model.ReleasePhase) {
	s.db.Model(&model.GPSReleasePlan{}).Where("id = ?", planID).Update("phase", string(phase))
}

func (s *Store) SetModuleStatus(planID, moduleID string, status model.ModuleStatus, errMsg string) {
	updates := map[string]interface{}{"status": string(status)}
	if errMsg != "" {
		updates["error_msg"] = errMsg
	}
	now := time.Now()
	if status == model.StatusReleasing {
		updates["start_time"] = &now
	}
	if status == model.StatusSuccess || status == model.StatusFailed || status == model.StatusSkipped {
		updates["end_time"] = &now
	}
	if status == model.StatusRetrying {
		s.db.Model(&model.GPSPlanModule{}).
			Where("plan_id = ? AND ga = ?", planID, moduleID).
			Update("retry_count", gorm.Expr("retry_count + 1"))
	}
	s.db.Model(&model.GPSPlanModule{}).
		Where("plan_id = ? AND ga = ?", planID, moduleID).
		Updates(updates)
}

func (s *Store) CompletePlan(planID string, status model.PlanStatus) {
	now := time.Now()
	s.db.Model(&model.GPSReleasePlan{}).Where("id = ?", planID).Updates(map[string]interface{}{
		"status": string(status), "phase": string(model.PhaseCompleted), "completed_at": &now,
	})

	var modules []model.GPSPlanModule
	s.db.Where("plan_id = ?", planID).Find(&modules)
	succeeded, failed, skipped := 0, 0, 0
	for _, m := range modules {
		switch model.ModuleStatus(m.Status) {
		case model.StatusSuccess:
			succeeded++
		case model.StatusFailed:
			failed++
		case model.StatusSkipped:
			skipped++
		}
	}

	var plan model.GPSReleasePlan
	s.db.First(&plan, "id = ?", planID)
	siloIDs := plan.SiloIDs.Data()
	siloNames := make([]string, 0, len(siloIDs))
	for _, sid := range siloIDs {
		if silo := s.GetSilo(sid); silo != nil {
			siloNames = append(siloNames, silo.Name)
		}
	}
	var duration string
	if plan.StartedAt != nil {
		d := now.Sub(*plan.StartedAt)
		duration = fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}

	s.db.Create(&model.GPSReleaseHistory{
		PlanID: planID, SiloIDs: datatypes.NewJSONType(siloIDs), SiloNames: datatypes.NewJSONType(siloNames),
		Status: string(status), TotalModules: len(modules), Succeeded: succeeded, Failed: failed, Skipped: skipped,
		Duration: duration, CreatedAt: plan.CreatedAt, CompletedAt: &now,
	})
}

func (s *Store) GetProgress(planID string) *model.ReleaseProgress {
	plan := s.GetPlan(planID)
	if plan == nil {
		return nil
	}
	p := &model.ReleaseProgress{
		PlanID: planID, Phase: plan.Phase, Status: plan.Status,
		TotalModules: len(plan.Modules), Modules: plan.Modules,
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

// --- History ---

func (s *Store) GetHistory() []model.HistoryEntry {
	var rows []model.GPSReleaseHistory
	s.db.Order("created_at DESC").Find(&rows)
	out := make([]model.HistoryEntry, len(rows))
	for i, r := range rows {
		out[i] = model.HistoryEntry{
			PlanID: r.PlanID, SiloIDs: r.SiloIDs.Data(), SiloNames: r.SiloNames.Data(),
			Status: model.PlanStatus(r.Status), TotalModules: r.TotalModules,
			Succeeded: r.Succeeded, Failed: r.Failed, Skipped: r.Skipped,
			Duration: r.Duration, CreatedAt: r.CreatedAt, CompletedAt: r.CompletedAt,
		}
	}
	return out
}

// --- Users & RBAC ---

func (s *Store) FindOrCreateUser(in *model.User) (*model.User, bool, error) {
	var existing model.GPSUser
	err := s.db.Where("username = ?", in.Username).First(&existing).Error
	if err == nil {
		updates := map[string]interface{}{}
		if in.GitlabID != 0 {
			updates["gitlab_id"] = in.GitlabID
		}
		if in.Name != "" {
			updates["name"] = in.Name
		}
		if in.Email != "" {
			updates["email"] = in.Email
		}
		if in.AvatarURL != "" {
			updates["avatar_url"] = in.AvatarURL
		}
		if len(updates) > 0 {
			s.db.Model(&existing).Updates(updates)
		}
		u := toUser(existing)
		return &u, false, nil
	}
	newUser := model.GPSUser{
		Username: in.Username, Name: in.Name, Email: in.Email, AvatarURL: in.AvatarURL, GitlabID: in.GitlabID,
		Roles: datatypes.NewJSONType([]string{model.RoleViewer}), AllowedSilos: "", CreatedAt: time.Now(),
	}
	if err := s.db.Create(&newUser).Error; err != nil {
		return nil, false, err
	}
	u := toUser(newUser)
	return &u, true, nil
}

func (s *Store) GetUserByUsername(username string) *model.User {
	var row model.GPSUser
	if err := s.db.Where("username = ?", username).First(&row).Error; err != nil {
		return nil
	}
	u := toUser(row)
	return &u
}

func (s *Store) GetUserByID(id int) *model.User {
	var row model.GPSUser
	if err := s.db.First(&row, id).Error; err != nil {
		return nil
	}
	u := toUser(row)
	return &u
}

func (s *Store) ListUsers() []*model.User {
	var rows []model.GPSUser
	s.db.Order("id").Find(&rows)
	out := make([]*model.User, len(rows))
	for i, r := range rows {
		u := toUser(r)
		out[i] = &u
	}
	return out
}

func (s *Store) CountUsers() int {
	var count int64
	s.db.Model(&model.GPSUser{}).Count(&count)
	return int(count)
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
	for _, r := range roles {
		var role model.GPSRole
		if err := s.db.First(&role, "name = ?", r).Error; err != nil {
			return false, fmt.Errorf("unknown role: %s", r)
		}
	}
	var count int64
	s.db.Model(&model.GPSUser{}).Where("username = ?", username).Count(&count)
	if count > 0 {
		return false, nil
	}
	s.db.Create(&model.GPSUser{
		Username: username, Email: strings.TrimSpace(entry.Email),
		Roles: datatypes.NewJSONType(roles), AllowedSilos: strings.TrimSpace(entry.AllowedSilos), CreatedAt: time.Now(),
	})
	return true, nil
}

func (s *Store) GetRoles() []*model.Role {
	var rows []model.GPSRole
	s.db.Order("name").Find(&rows)
	out := make([]*model.Role, len(rows))
	for i, r := range rows {
		out[i] = &model.Role{Name: r.Name, Description: r.Description, Actions: r.Actions.Data()}
	}
	return out
}

func (s *Store) SetUserRoles(userID int, roles []string) error {
	var user model.GPSUser
	if err := s.db.First(&user, userID).Error; err != nil {
		return fmt.Errorf("user not found")
	}
	for _, r := range roles {
		var role model.GPSRole
		if err := s.db.First(&role, "name = ?", r).Error; err != nil {
			return fmt.Errorf("unknown role: %s", r)
		}
	}
	s.db.Model(&user).Update("roles", datatypes.NewJSONType(roles))
	return nil
}

func (s *Store) SetUserAllowedSilos(userID int, allowedSilos string) error {
	result := s.db.Model(&model.GPSUser{}).Where("id = ?", userID).Update("allowed_silos", allowedSilos)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

func (s *Store) UserActions(u *model.User) map[string]bool {
	actions := make(map[string]bool)
	for _, roleName := range u.Roles {
		var role model.GPSRole
		if err := s.db.First(&role, "name = ?", roleName).Error; err != nil {
			continue
		}
		for _, a := range role.Actions.Data() {
			actions[a] = true
		}
	}
	return actions
}

// --- Helpers ---

func (s *Store) nextPlanID() string {
	var count int64
	s.db.Model(&model.GPSReleasePlan{}).Count(&count)
	return fmt.Sprintf("plan-%03d", count+1)
}

func bumpPatch(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return version
	}
	patch := 0
	fmt.Sscanf(parts[2], "%d", &patch)
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
}

func toRepos(rows []model.GPSRepo) []model.Repo {
	out := make([]model.Repo, len(rows))
	for i, r := range rows {
		out[i] = toRepo(r)
	}
	return out
}

func toRepo(r model.GPSRepo) model.Repo {
	return model.Repo{ID: r.ID, SiloID: r.SiloID, Name: r.Name, URL: r.URL, ReleaseBranch: r.ReleaseBranch}
}

func toUser(u model.GPSUser) model.User {
	roles := u.Roles.Data()
	if roles == nil {
		roles = []string{}
	}
	return model.User{
		ID: u.ID, Username: u.Username, Name: u.Name, Email: u.Email, AvatarURL: u.AvatarURL,
		GitlabID: u.GitlabID, Roles: roles, AllowedSilos: u.AllowedSilos, CreatedAt: u.CreatedAt,
	}
}
