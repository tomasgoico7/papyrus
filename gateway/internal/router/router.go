package router

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/app"
	"github.com/papyrus/gateway/internal/auth"
	"github.com/papyrus/gateway/internal/config"
	"github.com/papyrus/gateway/internal/handlers"
	"github.com/papyrus/gateway/internal/middleware"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
)

// New builds the HTTP engine. The metrics registry and the analyzer are passed
// in rather than created here: the worker shares both when it runs in this
// process, and two registries would mean half the numbers missing from
// /metrics.
func New(
	cfg *config.Config,
	logger *slog.Logger,
	metrics *observability.Metrics,
	analyzer services.Analyzer,
	queue handlers.JobQueue,
) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.MaxMultipartMemory = cfg.MaxUploadBytes
	engine.Use(
		gin.Recovery(),
		middleware.RequestID(logger),
		metrics.Middleware(),
		middleware.CORS(cfg.AllowedOrigins),
	)

	upstream := app.UpstreamClient(cfg.RequestTimeout)
	analyzeHandler := handlers.NewAnalyzeHandler(analyzer, cfg.MaxUploadBytes, cfg.RequestTimeout)
	tailor := services.NewTailorClient(cfg.AIServiceURL, cfg.AIServiceToken, upstream)
	tailorHandler := handlers.NewTailorHandler(tailor, cfg.MaxUploadBytes, cfg.RequestTimeout)
	keySet := auth.NewKeySet(cfg.JWKSURL)
	rateLimiter := app.RateLimiter(cfg, metrics, logger)

	engine.GET("/health", handlers.Health)
	engine.GET(metrics.Path(), gin.WrapH(metrics.Handler()))

	authed := engine.Group("/")
	authed.Use(middleware.Auth(keySet.Keyfunc), middleware.RateLimit(rateLimiter))
	authed.POST("/analyze", analyzeHandler.Handle)
	authed.POST("/tailor/questions", tailorHandler.Questions)
	authed.POST("/tailor/generate", tailorHandler.Generate)

	// The asynchronous path appears only where there is a queue behind it. The
	// synchronous endpoint stays either way, so a deployment without a database
	// keeps working exactly as before.
	if lookup, ok := analyzer.(handlers.AnalysisLookup); ok && queue != nil {
		analyses := handlers.NewAnalysesHandler(lookup, queue, cfg.MaxUploadBytes, cfg.RequestTimeout)
		authed.POST("/analyses", analyses.Submit)
		authed.GET("/analyses/:id", analyses.Status)
		logger.Info("asynchronous analyses enabled")
	}

	return engine
}
