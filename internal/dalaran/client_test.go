package dalaran

import "testing"

func TestMapTree(t *testing.T) {
	data := []businessSilo{
		{
			Code:   "SPIS",
			Name:   "发行业务",
			NameEn: "IssuanceBusiness",
			Repos: []codeRepo{
				{URL: "ssh://git@codeup.devops.csdc.com:9022/group/issuance.git", DevOpsOpt: true},
				{URL: "https://gitlab.local/platform/settle.git", DevOpsOpt: true},
				{URL: "https://gitlab.local/platform/legacy.git", DevOpsOpt: false}, // skipped: devops disabled
				{URL: "", DevOpsOpt: true},                                          // skipped: empty URL
			},
		},
		{
			Code:   "RISK",
			Name:   "风控",
			NameEn: "", // falls back to code
			Repos:  []codeRepo{{URL: "git@host:team/risk-core.git", DevOpsOpt: true}},
		},
	}

	silos, repos := mapTree(data)

	if len(silos) != 2 {
		t.Fatalf("silos = %d, want 2", len(silos))
	}
	if silos[0].ID != "SPIS" || silos[0].Name != "IssuanceBusiness" || silos[0].Desc != "发行业务" {
		t.Fatalf("silo[0] mapping wrong: %+v", silos[0])
	}
	if silos[1].Name != "RISK" {
		t.Fatalf("silo[1] name fallback wrong: %q, want RISK", silos[1].Name)
	}

	if len(repos) != 3 {
		t.Fatalf("repos = %d, want 3 (empty URL & non-devops skipped)", len(repos))
	}
	if repos[0].Name != "issuance" {
		t.Fatalf("repo[0] name = %q, want issuance", repos[0].Name)
	}
	if repos[0].SiloID != "SPIS" {
		t.Fatalf("repo[0] siloID = %q, want SPIS", repos[0].SiloID)
	}
	if repos[0].ReleaseBranch != "main" {
		t.Fatalf("repo[0] branch = %q, want main", repos[0].ReleaseBranch)
	}
	if repos[2].Name != "risk-core" || repos[2].SiloID != "RISK" {
		t.Fatalf("repo[2] mapping wrong: %+v", repos[2])
	}
	// IDs must be unique
	if repos[0].ID == repos[1].ID {
		t.Fatal("repo IDs not unique")
	}
}

func TestRepoNameFromURL(t *testing.T) {
	cases := map[string]string{
		"ssh://git@host:9022/group/issuance.git": "issuance",
		"https://gitlab.local/platform/settle.git": "settle",
		"git@host:team/risk-core.git":               "risk-core",
		"plainname":                                 "plainname",
	}
	for in, want := range cases {
		if got := repoNameFromURL(in); got != want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}
