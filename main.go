package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"gps/internal/auth"
	"gps/internal/dalaran"
	"gps/internal/handler"
	"gps/internal/middleware"
	"gps/internal/mock"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

//go:embed all:static
var staticFiles embed.FS

func main() {
	store := buildStore()
	simulator := mock.NewSimulator(store)

	// --- Auth service ---
	jwtSecret := os.Getenv("GPS_JWT_SECRET")
	if jwtSecret == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		jwtSecret = hex.EncodeToString(b)
		log.Println("GPS_JWT_SECRET not set; generated an ephemeral secret (sessions reset on restart)")
	}
	authService := auth.NewService(jwtSecret)
	if appID := os.Getenv("GPS_GITLAB_APP_ID"); appID != "" {
		gitlabURL := os.Getenv("GPS_GITLAB_URL")
		if gitlabURL == "" {
			gitlabURL = "https://gitlab.local"
		}
		authService.ConfigureGitlab(
			gitlabURL,
			appID,
			os.Getenv("GPS_GITLAB_APP_SECRET"),
			os.Getenv("GPS_GITLAB_CALLBACK_URL"),
		)
		log.Printf("GitLab SSO enabled: %s", gitlabURL)
	} else {
		log.Println("GitLab SSO not configured; use mock login (built-in admin)")
	}
	authMiddleware := middleware.NewAuthMiddleware(authService)

	siloHandler := handler.NewSiloHandler(store)
	repoHandler := handler.NewRepoHandler(store)
	planHandler := handler.NewPlanHandler(store)
	releaseHandler := handler.NewReleaseHandler(store, simulator)
	historyHandler := handler.NewHistoryHandler(store)
	authHandler := handler.NewAuthHandler(store, authService)
	adminHandler := handler.NewAdminHandler(store)

	r := gin.Default()

	// Auth routes (public)
	r.GET("/auth/login", authHandler.LoginPage)
	r.POST("/auth/mock-login", authHandler.MockLogin)
	r.GET("/auth/gitlab/callback", authHandler.GitlabCallback)

	// API routes (require authentication)
	api := r.Group("/api")
	api.Use(authMiddleware.RequireAuth())
	{
		// Session
		api.GET("/current-user", authHandler.CurrentUser)
		api.POST("/logout", authHandler.Logout)

		// User & role management (admin only — enforced in handlers)
		api.GET("/admin/users", adminHandler.ListUsers)
		api.GET("/admin/roles", adminHandler.ListRoles)
		api.POST("/admin/users/import", adminHandler.ImportUsers)
		api.PUT("/admin/users/:uid/roles", adminHandler.SetUserRoles)
		api.PUT("/admin/users/:uid/access", adminHandler.UpdateUserAccess)

		// Product tree
		api.GET("/silos", siloHandler.ListSilos)
		api.GET("/silos/:id/repos", siloHandler.GetReposBySilo)
		api.GET("/repos/:id/modules", siloHandler.GetModulesByRepo)

		// Repos (full list + release-branch config)
		api.GET("/repos", repoHandler.ListRepos)
		api.PUT("/repos/:id/branch", repoHandler.UpdateRepoBranch)

		// Plans
		api.POST("/plans", planHandler.CreatePlan)
		api.GET("/plans", planHandler.ListPlans)
		api.GET("/plans/:id", planHandler.GetPlan)
		api.PUT("/plans/:id/versions", planHandler.UpdateVersions)
		api.POST("/plans/:id/confirm", planHandler.ConfirmPlan)

		// Release execution
		api.POST("/plans/:id/execute", releaseHandler.Execute)
		api.GET("/plans/:id/progress", releaseHandler.GetProgress)
		api.GET("/plans/:id/events", releaseHandler.SSEEvents)
		api.POST("/plans/:id/abort", releaseHandler.Abort)
		api.POST("/plans/:id/modules/:mid/retry", releaseHandler.RetryModule)

		// History
		api.GET("/history", historyHandler.ListHistory)
		api.GET("/history/:id", historyHandler.GetHistoryDetail)
	}

	// Serve embedded static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}

	serveIndex := func(c *gin.Context) {
		data, err := fs.ReadFile(staticFS, "index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "index.html not found")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	}

	// Serve index.html at root
	r.GET("/", serveIndex)

	// Serve static assets under known paths
	r.GET("/css/*filepath", func(c *gin.Context) {
		c.FileFromFS("css"+c.Param("filepath"), http.FS(staticFS))
	})
	r.GET("/js/*filepath", func(c *gin.Context) {
		c.FileFromFS("js"+c.Param("filepath"), http.FS(staticFS))
	})
	r.GET("/lib/*filepath", func(c *gin.Context) {
		c.FileFromFS("lib"+c.Param("filepath"), http.FS(staticFS))
	})

	// SPA fallback: any other route → index.html
	r.NoRoute(serveIndex)

	port := 4777
	fmt.Printf("\n  GPS Frontend Prototype\n")
	fmt.Printf("  ========================\n")
	fmt.Printf("  Server running at: http://localhost:%d\n\n", port)

	log.Fatal(r.Run(fmt.Sprintf(":%d", port)))
}

// buildStore loads the silo/repo product tree from dalaran. dalaran is the sole
// source of silo/repo data, so a missing config or a failed fetch is fatal.
// Modules and the dependency graph are always synthesized GPS-side (dalaran
// module info is not used).
func buildStore() *mock.Store {
	baseURL := os.Getenv("GPS_DALARAN_URL")
	if baseURL == "" {
		log.Fatal("GPS_DALARAN_URL is required: dalaran is the source of silo/repo data")
	}

	client := dalaran.NewClient(baseURL)
	silos, repos, err := client.FetchTree()
	if err != nil {
		log.Fatalf("dalaran fetch failed: %v", err)
	}
	if len(silos) == 0 {
		log.Fatal("dalaran returned no silos")
	}
	log.Printf("loaded product tree from dalaran: %d silos, %d repos (modules synthesized locally)", len(silos), len(repos))
	return mock.NewStoreWithTree(silos, repos)
}
