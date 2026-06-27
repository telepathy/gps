package mock

import (
	"testing"

	"gps/internal/model"
)

// storeWithURLs builds a store with repos that exercise name collision scenarios.
func storeWithURLs() *Store {
	silos := []model.Silo{
		{ID: "silo-001", Name: "framework", Desc: "Framework silo"},
		{ID: "silo-002", Name: "tools", Desc: "Tools silo"},
	}
	repos := []model.Repo{
		{
			ID: "repo-0001", SiloID: "silo-001",
			Name: "newclear-framework",
			URL:  "ssh://git@host:9022/framework/newclear-framework.git",
			ReleaseBranch: "release/3.2",
		},
		{
			ID: "repo-0002", SiloID: "silo-001",
			Name: "common-lib",
			URL:  "https://gitlab.local/framework/common-lib.git",
			ReleaseBranch: "main",
		},
		// Same name as repo-0002 but different group — collision scenario.
		{
			ID: "repo-0003", SiloID: "silo-002",
			Name: "common-lib",
			URL:  "https://gitlab.local/tools/common-lib.git",
			ReleaseBranch: "release/2.0",
		},
		// Repo with no / in path — repo name equals path segment.
		{
			ID: "repo-0004", SiloID: "silo-002",
			Name: "standalone",
			URL:  "https://github.com/standalone.git",
			ReleaseBranch: "master",
		},
	}
	return NewStoreWithTree(silos, repos)
}

func TestFindRepoByPathUniqueName(t *testing.T) {
	s := storeWithURLs()

	// Unique name — no collision.
	repo := s.FindRepoByPath("framework/newclear-framework")
	if repo == nil {
		t.Fatal("expected repo for newclear-framework")
	}
	if repo.ReleaseBranch != "release/3.2" {
		t.Fatalf("branch = %q, want release/3.2", repo.ReleaseBranch)
	}
	if repo.ID != "repo-0001" {
		t.Fatalf("id = %q, want repo-0001", repo.ID)
	}
}

func TestFindRepoByPathDisambiguation(t *testing.T) {
	s := storeWithURLs()

	// "common-lib" exists in both framework and tools groups.
	// Matching by URL path should pick the correct one.
	repo := s.FindRepoByPath("tools/common-lib")
	if repo == nil {
		t.Fatal("expected repo for tools/common-lib")
	}
	if repo.ID != "repo-0003" {
		t.Fatalf("id = %q, want repo-0003 (tools group)", repo.ID)
	}
	if repo.ReleaseBranch != "release/2.0" {
		t.Fatalf("branch = %q, want release/2.0", repo.ReleaseBranch)
	}

	// The other group should also resolve correctly.
	repo2 := s.FindRepoByPath("framework/common-lib")
	if repo2 == nil {
		t.Fatal("expected repo for framework/common-lib")
	}
	if repo2.ID != "repo-0002" {
		t.Fatalf("id = %q, want repo-0002 (framework group)", repo2.ID)
	}
}

func TestFindRepoByPathDisambiguationNoURLMatch(t *testing.T) {
	s := storeWithURLs()

	// Name "common-lib" exists, but the URL path doesn't match any repo.
	repo := s.FindRepoByPath("unknown/common-lib")
	if repo != nil {
		t.Fatalf("expected nil for unknown/common-lib, got %+v", repo)
	}
}

func TestFindRepoByPathNotFound(t *testing.T) {
	s := storeWithURLs()

	repo := s.FindRepoByPath("nonexistent/repo")
	if repo != nil {
		t.Fatalf("expected nil for nonexistent repo, got %+v", repo)
	}
}

func TestFindRepoByPathNameOnly(t *testing.T) {
	s := storeWithURLs()

	// repositoryPath with no slash — the entire string is the name.
	repo := s.FindRepoByPath("standalone")
	if repo == nil {
		t.Fatal("expected repo for standalone")
	}
	if repo.ID != "repo-0004" {
		t.Fatalf("id = %q, want repo-0004", repo.ID)
	}
	if repo.ReleaseBranch != "master" {
		t.Fatalf("branch = %q, want master", repo.ReleaseBranch)
	}
}

func TestFindRepoByPathEmptyStore(t *testing.T) {
	s := NewStoreWithTree(nil, nil)

	repo := s.FindRepoByPath("anything/repo")
	if repo != nil {
		t.Fatal("expected nil from empty store")
	}
}
