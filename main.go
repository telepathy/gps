package main

import (
	"embed"
	"fmt"
	"gps/internal/handler"
	"gps/internal/mock"
	"io/fs"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed all:static
var staticFiles embed.FS

func main() {
	store := mock.NewStore()
	simulator := mock.NewSimulator(store)

	siloHandler := handler.NewSiloHandler(store)
	planHandler := handler.NewPlanHandler(store)
	releaseHandler := handler.NewReleaseHandler(store, simulator)
	historyHandler := handler.NewHistoryHandler(store)

	r := gin.Default()

	// API routes
	api := r.Group("/api")
	{
		// Product tree
		api.GET("/silos", siloHandler.ListSilos)
		api.GET("/silos/:id/repos", siloHandler.GetReposBySilo)
		api.GET("/repos/:id/modules", siloHandler.GetModulesByRepo)

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

	port := 8080
	fmt.Printf("\n  GPS Frontend Prototype\n")
	fmt.Printf("  ========================\n")
	fmt.Printf("  Server running at: http://localhost:%d\n\n", port)

	log.Fatal(r.Run(fmt.Sprintf(":%d", port)))
}
