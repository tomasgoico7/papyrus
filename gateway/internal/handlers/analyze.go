package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
)

// AnalyzeHandler serves the synchronous analyze endpoint: the caller waits for
// the model. It is kept while the asynchronous path proves itself, and is the
// one to remove once clients have moved over.
type AnalyzeHandler struct {
	analyzer       services.Analyzer
	maxUploadBytes int64
	requestTimeout time.Duration
}

func NewAnalyzeHandler(analyzer services.Analyzer, maxUploadBytes int64, requestTimeout time.Duration) *AnalyzeHandler {
	return &AnalyzeHandler{
		analyzer:       analyzer,
		maxUploadBytes: maxUploadBytes,
		requestTimeout: requestTimeout,
	}
}

func (h *AnalyzeHandler) Handle(c *gin.Context) {
	upload, ok := parseAnalysisUpload(c, h.maxUploadBytes)
	if !ok {
		return
	}
	defer upload.File.Close()

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	analysis, err := h.analyzer.Analyze(ctx, services.AnalyzeRequest{
		CV:       upload.File,
		Filename: upload.Filename,
		JobOffer: upload.JobOffer,
		JobTitle: upload.JobTitle,
	})
	if err != nil {
		respondUpstream(c, err, "analyze")
		return
	}

	c.JSON(http.StatusOK, transport.AnalysisResponse{
		Analysis:   *analysis,
		CVFilename: upload.Filename,
	})
}
