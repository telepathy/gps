package model

import "strings"

// UrlMatchesPath checks whether a repo URL's path portion matches the given
// repositoryPath after stripping protocol, host, and optional .git suffix.
//
//	e.g. "ssh://git@host:9022/framework/newclear-framework.git" → "framework/newclear-framework"
func UrlMatchesPath(url, repositoryPath string) bool {
	s := url
	// Strip protocol: "ssh://git@host:9022/..." → "git@host:9022/..."
	if idx := strings.Index(s, "://"); idx >= 0 {
		s = s[idx+3:]
	}
	// Strip user@host:port: "git@host:9022/..." → "host:9022/..."
	if idx := strings.Index(s, "@"); idx >= 0 {
		s = s[idx+1:]
	}
	// Strip host:port: "host:9022/..." → "framework/..."
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[idx+1:]
	}
	s = strings.TrimSuffix(s, ".git")
	return s == repositoryPath
}
