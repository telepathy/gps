package auth

import (
	"crypto/tls"
	"fmt"
	"log"
	"time"

	"gps/internal/model"

	"github.com/go-resty/resty/v2"
	"github.com/golang-jwt/jwt/v5"
)

// Service handles GitLab OAuth and JWT session tokens.
type Service struct {
	jwtSecret []byte

	gitlabURL   string
	appID       string
	appSecret   string
	callbackURL string
}

func NewService(jwtSecret string) *Service {
	return &Service{jwtSecret: []byte(jwtSecret)}
}

// ConfigureGitlab enables GitLab SSO. Called only when an app ID is supplied.
func (s *Service) ConfigureGitlab(url, appID, appSecret, callbackURL string) {
	s.gitlabURL = url
	s.appID = appID
	s.appSecret = appSecret
	s.callbackURL = callbackURL
}

func (s *Service) IsGitlabConfigured() bool {
	return s.appID != "" && s.gitlabURL != ""
}

// GitlabAuthURL is the authorization endpoint the browser is redirected to.
func (s *Service) GitlabAuthURL() string {
	return fmt.Sprintf(
		"%s/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=read_user",
		s.gitlabURL, s.appID, s.callbackURL,
	)
}

// newClient builds a resty client that skips TLS verification — the target
// GitLab is a self-signed internal instance.
func (s *Service) newClient() *resty.Client {
	c := resty.New()
	c.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}) // 自签名实例，跳过证书校验
	return c
}

// ExchangeCode swaps an OAuth code for a token, then fetches the GitLab user.
// GPS only consumes the username (plus optional email/avatar for display).
func (s *Service) ExchangeCode(code string) (*model.GitlabUser, error) {
	client := s.newClient()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	resp, err := client.R().
		SetFormData(map[string]string{
			"client_id":     s.appID,
			"client_secret": s.appSecret,
			"code":          code,
			"grant_type":    "authorization_code",
			"redirect_uri":  s.callbackURL,
		}).
		SetResult(&tokenResp).
		Post(s.gitlabURL + "/oauth/token")
	if err != nil {
		log.Printf("[GitLab SSO] exchange code error: %v", err)
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("exchange code: gitlab returned %d: %s", resp.StatusCode(), resp.String())
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("empty access_token from gitlab")
	}

	var gitlabUser model.GitlabUser
	resp, err = client.R().
		SetHeader("Authorization", "Bearer "+tokenResp.AccessToken).
		SetResult(&gitlabUser).
		Get(s.gitlabURL + "/api/v4/user")
	if err != nil {
		log.Printf("[GitLab SSO] get user info error: %v", err)
		return nil, fmt.Errorf("get user info: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("get user info: gitlab returned %d: %s", resp.StatusCode(), resp.String())
	}
	if gitlabUser.Username == "" {
		return nil, fmt.Errorf("empty username in gitlab response: %s", resp.String())
	}
	log.Printf("[GitLab SSO] authenticated user: %s (id=%d)", gitlabUser.Username, gitlabUser.ID)
	return &gitlabUser, nil
}

// GenerateToken issues a 24h HS256 JWT for a user.
func (s *Service) GenerateToken(u *model.User) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  u.ID,
		"username": u.Username,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

// ParseToken validates a JWT and returns (userID, username).
func (s *Service) ParseToken(tokenStr string) (int, string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return 0, "", err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return 0, "", fmt.Errorf("invalid token")
	}
	uid, _ := claims["user_id"].(float64)
	username, _ := claims["username"].(string)
	return int(uid), username, nil
}
