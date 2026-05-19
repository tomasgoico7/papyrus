// Package router wires configuration, middleware and handlers into a ready-to-
// serve gin engine. Keeping assembly here keeps main.go focused on process
// lifecycle.
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/auth"
	"github.com/papyrus/gateway/internal/config"
	"github.com/papyrus/gateway/internal/handlers"
	"github.com/papyrus/gateway/internal/middleware"
	"github.com/papyrus/gateway/internal/services"
)

func New(cfg *config.Config) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.MaxMultipartMemory = cfg.MaxUploadBytes
	engine.Use(gin.Recovery(), middleware.CORS(cfg.AllowedOrigins))

	// The context deadline set per request governs upstream cancellation, so the
	// shared client itself is left without a blanket timeout.
	analyzer := services.NewAnalyzerClient(cfg.AIServiceURL, &http.Client{})
	analyzeHandler := handlers.NewAnalyzeHandler(analyzer, cfg.MaxUploadBytes, cfg.RequestTimeout)
	keySet := auth.NewKeySet(cfg.JWKSURL)
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimitRPM)

	engine.GET("/health", handlers.Health)

	authed := engine.Group("/")
	authed.Use(middleware.Auth(keySet.Keyfunc), rateLimiter.Middleware())
	authed.POST("/analyze", analyzeHandler.Handle)

	return engine
}
