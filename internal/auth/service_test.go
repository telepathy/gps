package auth

import (
	"testing"

	"gps/internal/model"
)

func TestJWTRoundTrip(t *testing.T) {
	s := NewService("secret")
	u := &model.User{ID: 7, Username: "alice"}
	tok, err := s.GenerateToken(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	id, name, err := s.ParseToken(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id != 7 || name != "alice" {
		t.Fatalf("got id=%d name=%s, want 7/alice", id, name)
	}
}

func TestParseTokenRejectsTampered(t *testing.T) {
	s := NewService("secret")
	if _, _, err := s.ParseToken("not.a.jwt"); err == nil {
		t.Fatal("expected error for garbage token")
	}
	other := NewService("different-secret")
	tok, _ := other.GenerateToken(&model.User{ID: 1, Username: "x"})
	if _, _, err := s.ParseToken(tok); err == nil {
		t.Fatal("expected error for wrong-secret token")
	}
}

func TestGitlabAuthURL(t *testing.T) {
	s := NewService("secret")
	s.ConfigureGitlab("https://gitlab.local", "appid", "appsecret", "https://gps/cb")
	if !s.IsGitlabConfigured() {
		t.Fatal("expected configured")
	}
	got := s.GitlabAuthURL()
	want := "https://gitlab.local/oauth/authorize?client_id=appid&redirect_uri=https://gps/cb&response_type=code&scope=read_user"
	if got != want {
		t.Fatalf("auth url = %q, want %q", got, want)
	}
}
