package mock

import (
	"fmt"
	"strings"
	"testing"

	"gps/internal/model"
)

// newTestStore builds a store with synthetic silo/repo data, mirroring how
// NewStoreWithTree is fed from dalaran in production.
func newTestStore() *Store {
	var silos []model.Silo
	var repos []model.Repo
	for i := 1; i <= 8; i++ {
		sid := fmt.Sprintf("silo-%03d", i)
		silos = append(silos, model.Silo{ID: sid, Name: fmt.Sprintf("silo%d", i), Desc: "test"})
		repos = append(repos, model.Repo{
			ID:            fmt.Sprintf("repo-%03d", i),
			SiloID:        sid,
			Name:          fmt.Sprintf("repo%d", i),
			URL:           fmt.Sprintf("git@host:team/repo%d.git", i),
			ReleaseBranch: "main",
		})
	}
	return NewStoreWithTree(silos, repos)
}

func TestSeededRolesAndAdmin(t *testing.T) {
	s := newTestStore()

	if got := len(s.GetRoles()); got != 3 {
		t.Fatalf("roles = %d, want 3", got)
	}
	admin := s.GetUserByUsername("admin")
	if admin == nil {
		t.Fatal("embedded admin missing")
	}
	if admin.AllowedSilos != "*" {
		t.Fatalf("admin AllowedSilos = %q, want *", admin.AllowedSilos)
	}
	acts := s.UserActions(admin)
	for _, a := range []string{model.ActionManage, model.ActionCreate, model.ActionRelease, model.ActionView} {
		if !acts[a] {
			t.Fatalf("admin missing action %s", a)
		}
	}
}

func TestFindOrCreateDefaultsToViewer(t *testing.T) {
	s := newTestStore()
	u, isNew, err := s.FindOrCreateUser(&model.User{Username: "bob", GitlabID: 99})
	if err != nil || !isNew {
		t.Fatalf("create bob: isNew=%v err=%v", isNew, err)
	}
	if len(u.Roles) != 1 || u.Roles[0] != model.RoleViewer {
		t.Fatalf("default roles = %v, want [viewer]", u.Roles)
	}
	acts := s.UserActions(u)
	if acts[model.ActionCreate] || acts[model.ActionRelease] || acts[model.ActionManage] {
		t.Fatal("viewer should only have view")
	}
	if !acts[model.ActionView] {
		t.Fatal("viewer should have view")
	}

	// Re-login keeps roles, refreshes gitlab fields.
	again, isNew2, _ := s.FindOrCreateUser(&model.User{Username: "bob", Email: "bob@x"})
	if isNew2 {
		t.Fatal("second find should not be new")
	}
	if again.Email != "bob@x" {
		t.Fatalf("email not refreshed: %q", again.Email)
	}
}

func TestSetUserRolesValidation(t *testing.T) {
	s := newTestStore()
	u, _, _ := s.FindOrCreateUser(&model.User{Username: "carol"})
	if err := s.SetUserRoles(u.ID, []string{"nonexistent"}); err == nil {
		t.Fatal("expected error for unknown role")
	}
	if err := s.SetUserRoles(u.ID, []string{model.RoleReleaser}); err != nil {
		t.Fatalf("set releaser: %v", err)
	}
	if !s.UserActions(s.GetUserByID(u.ID))[model.ActionRelease] {
		t.Fatal("releaser should have release action")
	}
}

// mirrors handler.canReleaseSilos to lock in semantics
func canRelease(u *model.User, silos []string) bool {
	for _, r := range u.Roles {
		if r == model.RoleAdmin {
			return true
		}
	}
	if u.AllowedSilos == "*" {
		return true
	}
	allowed := map[string]bool{}
	for _, s := range strings.Split(u.AllowedSilos, ",") {
		if s = strings.TrimSpace(s); s != "" {
			allowed[s] = true
		}
	}
	for _, w := range silos {
		if !allowed[w] {
			return false
		}
	}
	return true
}

func TestSiloScope(t *testing.T) {
	admin := &model.User{Roles: []string{model.RoleAdmin}, AllowedSilos: ""}
	if !canRelease(admin, []string{"silo-001"}) {
		t.Fatal("admin should pass any silo")
	}
	scoped := &model.User{Roles: []string{model.RoleReleaser}, AllowedSilos: "silo-001,silo-002"}
	if !canRelease(scoped, []string{"silo-001"}) {
		t.Fatal("scoped should pass allowed silo")
	}
	if canRelease(scoped, []string{"silo-003"}) {
		t.Fatal("scoped should fail disallowed silo")
	}
	if canRelease(scoped, []string{"silo-001", "silo-003"}) {
		t.Fatal("scoped should fail if any silo disallowed")
	}
}

func TestRepoBranchConfig(t *testing.T) {
	s := newTestStore()
	all := s.GetAllRepos()
	if len(all) == 0 {
		t.Fatal("expected mock repos")
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].SiloID > all[i].SiloID {
			t.Fatal("repos not sorted by silo")
		}
	}

	target := all[0].ID
	updated, err := s.UpdateRepoBranch(target, "release/2099Q9")
	if err != nil {
		t.Fatalf("update branch: %v", err)
	}
	if updated.ReleaseBranch != "release/2099Q9" {
		t.Fatalf("branch = %q, want release/2099Q9", updated.ReleaseBranch)
	}
	if s.GetRepo(target).ReleaseBranch != "release/2099Q9" {
		t.Fatal("branch not persisted in store")
	}
	if _, err := s.UpdateRepoBranch("nonexistent", "x"); err == nil {
		t.Fatal("expected error for unknown repo")
	}
}

func TestImportUser(t *testing.T) {
	s := newTestStore()

	// New pre-registered user with explicit role + silo scope.
	created, err := s.ImportUser(model.ImportUserEntry{
		Username:     "dave",
		Roles:        []string{model.RoleReleaser},
		AllowedSilos: "silo-001",
	})
	if err != nil || !created {
		t.Fatalf("import dave: created=%v err=%v", created, err)
	}
	u := s.GetUserByUsername("dave")
	if u == nil || u.GitlabID != 0 {
		t.Fatalf("imported user should exist with GitlabID=0: %+v", u)
	}
	if len(u.Roles) != 1 || u.Roles[0] != model.RoleReleaser {
		t.Fatalf("roles = %v, want [releaser]", u.Roles)
	}

	// Re-import existing username is skipped (not overwritten).
	created, err = s.ImportUser(model.ImportUserEntry{Username: "dave", Roles: []string{model.RoleAdmin}})
	if err != nil || created {
		t.Fatalf("re-import should skip: created=%v err=%v", created, err)
	}
	if s.GetUserByUsername("dave").Roles[0] != model.RoleReleaser {
		t.Fatal("re-import must not overwrite roles")
	}

	// Empty roles defaults to viewer.
	_, _ = s.ImportUser(model.ImportUserEntry{Username: "erin"})
	if s.GetUserByUsername("erin").Roles[0] != model.RoleViewer {
		t.Fatal("empty roles should default to viewer")
	}

	// Unknown role rejected.
	if _, err := s.ImportUser(model.ImportUserEntry{Username: "frank", Roles: []string{"ghost"}}); err == nil {
		t.Fatal("unknown role should fail")
	}
	if s.GetUserByUsername("frank") != nil {
		t.Fatal("failed import must not create user")
	}

	// SSO login binds GitLab info to the pre-registered record, preserving roles.
	bound, isNew, _ := s.FindOrCreateUser(&model.User{Username: "dave", GitlabID: 555, Email: "dave@gl"})
	if isNew {
		t.Fatal("SSO login of imported user should not be new")
	}
	if bound.GitlabID != 555 || bound.Email != "dave@gl" {
		t.Fatalf("GitLab info not bound: %+v", bound)
	}
	if bound.Roles[0] != model.RoleReleaser || bound.AllowedSilos != "silo-001" {
		t.Fatalf("SSO bind must preserve imported roles/silos: %+v", bound)
	}
}
