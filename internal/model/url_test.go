package model

import "testing"

func TestUrlMatchesPath(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		path       string
		want       bool
	}{
		{
			name: "ssh URL with @ and port",
			url:  "ssh://git@host:9022/framework/newclear-framework.git",
			path: "framework/newclear-framework",
			want: true,
		},
		{
			name: "HTTPS URL no auth",
			url:  "https://github.com/telepathy/gps.git",
			path: "telepathy/gps",
			want: true,
		},
		{
			name: "HTTP URL with port and subgroup",
			url:  "http://gitlab.local:8080/group/subgroup/repo.git",
			path: "group/subgroup/repo",
			want: true,
		},
		{
			name: "no .git suffix",
			url:  "https://host/group/repo",
			path: "group/repo",
			want: true,
		},
		{
			name: "path mismatch",
			url:  "ssh://git@host/repo.git",
			path: "other/repo",
			want: false,
		},
		{
			name: "name matches but group differs",
			url:  "ssh://git@host:9022/framework/newclear-framework.git",
			path: "other/newclear-framework",
			want: false,
		},
		{
			name: "protocol only (no / after host)",
			url:  "https://github.com",
			path: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UrlMatchesPath(tt.url, tt.path)
			if got != tt.want {
				t.Errorf("UrlMatchesPath(%q, %q) = %v, want %v", tt.url, tt.path, got, tt.want)
			}
		})
	}
}
