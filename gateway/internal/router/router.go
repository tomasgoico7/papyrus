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

func New(cfg *config.Config, logger *slog.Logger) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.MaxMultipartMemory = cfg.MaxUploadBytes
	metrics := observability.NewMetrics()
	engine.Use(
		gin.Recovery(),
		middleware.RequestID(logger),
		metrics.Middleware(),
		middleware.CORS(cfg.AllowedOrigins),
	)

	upstream := app.UpstreamClient(cfg.RequestTimeout)
	analyzer := app.Analyzer(cfg, upstream, metrics, logger)
	analyzeHandler := handlers.NewAnalyzeHandler(analyzer, cfg.MaxUploadBytes, cfg.RequestTimeout)
	tailor := services.NewTailorClient(cfg.AIServiceURL, cfg.AIServiceToken, upstream)
	tailorHandler := handlers.NewTailorHandler(tailor, cfg.MaxUploadBytes, cfg.RequestTimeout)
	keySet := auth.NewKeySet(cfg.JWKSURL)
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimitRPM)

	engine.GET("/health", handlers.Health)
	engine.GET(metrics.Path(), gin.WrapH(metrics.Handler()))

	authed := engine.Group("/")
	authed.Use(middleware.Auth(keySet.Keyfunc), rateLimiter.Middleware())
	authed.POST("/analyze", analyzeHandler.Handle)
	authed.POST("/tailor/questions", tailorHandler.Questions)
	authed.POST("/tailor/generate", tailorHandler.Generate)

	return engine
}
