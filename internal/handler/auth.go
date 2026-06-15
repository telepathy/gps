package handler

import (
	"html/template"
	"net/http"

	"gps/internal/auth"
	"gps/internal/model"
	"gps/internal/store"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	store       store.Store
	authService *auth.Service
}

func NewAuthHandler(store store.Store, authService *auth.Service) *AuthHandler {
	return &AuthHandler{store: store, authService: authService}
}

const cookieMaxAge = 86400 // 24h

var loginTmpl = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>登录 - GPS</title>
<style>
  body{margin:0;font-family:system-ui,-apple-system,sans-serif;background:#0d1117;color:#e6edf3;
       display:flex;align-items:center;justify-content:center;height:100vh}
  .box{background:#161b22;border:1px solid #30363d;border-radius:12px;padding:40px;width:340px;text-align:center}
  .logo{font-size:28px;font-weight:700;color:#58a6ff;letter-spacing:2px}
  .sub{color:#8b949e;font-size:13px;margin:6px 0 28px}
  .btn{display:block;width:100%;box-sizing:border-box;padding:11px;border-radius:8px;border:none;
       font-size:14px;cursor:pointer;margin-top:12px;text-decoration:none}
  .btn-gl{background:#fc6d26;color:#fff}
  .btn-mock{background:#238636;color:#fff}
  .err{background:#3d1418;border:1px solid #f85149;color:#f85149;padding:10px;border-radius:6px;
       font-size:13px;margin-bottom:18px}
  .hint{color:#8b949e;font-size:12px;margin-top:18px}
  input{width:100%;box-sizing:border-box;padding:10px;border-radius:6px;border:1px solid #30363d;
        background:#0d1117;color:#e6edf3;margin-top:12px}
  .divider{color:#8b949e;font-size:12px;margin:22px 0 4px}
</style></head>
<body><div class="box">
  <div class="logo">GPS</div>
  <div class="sub">Global Publishing System</div>
  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
  {{if .GitlabEnabled}}
    <a class="btn btn-gl" href="{{.GitlabAuthURL}}">使用 GitLab 账号登录</a>
    <div class="divider">— 或使用内置账号 —</div>
  {{else}}
    <div class="hint">GitLab SSO 未配置，使用内置账号登录</div>
  {{end}}
  <form method="POST" action="/auth/mock-login">
    <input name="username" placeholder="用户名 (内置: admin)" value="admin" autocomplete="off"/>
    <button class="btn btn-mock" type="submit">登录</button>
  </form>
</div></body></html>`))

// LoginPage renders the standalone login page (not part of the SPA).
func (h *AuthHandler) LoginPage(c *gin.Context) {
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = loginTmpl.Execute(c.Writer, gin.H{
		"Error":         c.Query("error"),
		"GitlabEnabled": h.authService.IsGitlabConfigured(),
		"GitlabAuthURL": h.authService.GitlabAuthURL(),
	})
}

// MockLogin authenticates against an existing in-memory user without GitLab.
// Bootstrap path: the embedded `admin` account is always present.
func (h *AuthHandler) MockLogin(c *gin.Context) {
	username := c.PostForm("username")
	if username == "" {
		var req model.MockLoginRequest
		_ = c.ShouldBindJSON(&req)
		username = req.Username
	}
	if username == "" {
		c.Redirect(http.StatusFound, "/auth/login?error="+template.URLQueryEscaper("用户名不能为空"))
		return
	}
	user := h.store.GetUserByUsername(username)
	if user == nil {
		c.Redirect(http.StatusFound, "/auth/login?error="+template.URLQueryEscaper("用户不存在: "+username))
		return
	}
	h.issueTokenAndRedirect(c, user)
}

// GitlabCallback handles the OAuth redirect from a self-signed GitLab instance.
func (h *AuthHandler) GitlabCallback(c *gin.Context) {
	if errMsg := c.Query("error"); errMsg != "" {
		c.Redirect(http.StatusFound, "/auth/login?error="+template.URLQueryEscaper("GitLab授权失败: "+errMsg))
		return
	}
	code := c.Query("code")
	if code == "" {
		c.Redirect(http.StatusFound, "/auth/login?error="+template.URLQueryEscaper("授权码缺失"))
		return
	}
	gitlabUser, err := h.authService.ExchangeCode(code)
	if err != nil {
		c.Redirect(http.StatusFound, "/auth/login?error="+template.URLQueryEscaper("GitLab认证失败: "+err.Error()))
		return
	}
	email := gitlabUser.Email
	if email == "" {
		email = gitlabUser.Username + "@gitlab.local"
	}
	user, _, err := h.store.FindOrCreateUser(&model.User{
		Username:  gitlabUser.Username,
		Email:     email,
		GitlabID:  gitlabUser.ID,
		AvatarURL: gitlabUser.Avatar,
	})
	if err != nil {
		c.Redirect(http.StatusFound, "/auth/login?error="+template.URLQueryEscaper("用户创建失败: "+err.Error()))
		return
	}
	h.issueTokenAndRedirect(c, user)
}

func (h *AuthHandler) issueTokenAndRedirect(c *gin.Context, user *model.User) {
	token, err := h.authService.GenerateToken(user)
	if err != nil {
		c.Redirect(http.StatusFound, "/auth/login?error="+template.URLQueryEscaper("生成token失败"))
		return
	}
	c.SetCookie("token", token, cookieMaxAge, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}

// Logout clears the session cookie.
func (h *AuthHandler) Logout(c *gin.Context) {
	c.SetCookie("token", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// CurrentUser returns the authenticated user (with roles).
func (h *AuthHandler) CurrentUser(c *gin.Context) {
	u := currentUser(c, h.store)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not logged in"})
		return
	}
	c.JSON(http.StatusOK, u)
}
