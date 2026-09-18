package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/auth"
	"github.com/papyrus/gateway/internal/config"
	"github.com/papyrus/gateway/internal/handlers"
	"github.com/papyrus/gateway/internal/middleware"
	"github.com/papyrus/gateway/internal/services"
)

func New(cfg *config.Config, logger *slog.Logger) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.MaxMultipartMemory = cfg.MaxUploadBytes
	engine.Use(
		gin.Recovery(),
		middleware.RequestID(logger),
		middleware.CORS(cfg.AllowedOrigins),
	)

	upstream := upstreamClient(cfg.RequestTimeout)
	analyzer := services.NewAnalyzerClient(cfg.AIServiceURL, cfg.AIServiceToken, upstream)
	analyzeHandler := handlers.NewAnalyzeHandler(analyzer, cfg.MaxUploadBytes, cfg.RequestTimeout)
	tailor := services.NewTailorClient(cfg.AIServiceURL, cfg.AIServiceToken, upstream)
	tailorHandler := handlers.NewTailorHandler(tailor, cfg.MaxUploadBytes, cfg.RequestTimeout)
	keySet := auth.NewKeySet(cfg.JWKSURL)
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimitRPM)

	engine.GET("/health", handlers.Health)

	authed := engine.Group("/")
	authed.Use(middleware.Auth(keySet.Keyfunc), rateLimiter.Middleware())
	authed.POST("/analyze", analyzeHandler.Handle)
	authed.POST("/tailor/questions", tailorHandler.Questions)
	authed.POST("/tailor/generate", tailorHandler.Generate)

	return engine
}

// upstreamClient is shared by both AI-service clients so they pool connections
// to the single upstream instead of keeping two pools of two.
//
// The default transport caps idle connections per host at 2, which under any
// concurrency forces a fresh dial — and a fresh TLS handshake — per request.
// The client timeout sits just above the per-request context deadline so the
// context still wins the race and callers get a 504 rather than a bare
// transport error.
func upstreamClient(requestTimeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 100
	transport.IdleConnTimeout = 90 * time.Second

	return &http.Client{
		Transport: transport,
		Timeout:   requestTimeout + 5*time.Second,
	}
}
