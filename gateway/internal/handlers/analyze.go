package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/httpx"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
)

const (
	minJobOfferLength = 40
	// Headroom over the file limit for the job-posting text and multipart framing.
	multipartOverhead = 1 << 20
)

type AnalyzeHandler struct {
	analyzer       *services.AnalyzerClient
	maxUploadBytes int64
	requestTimeout time.Duration
}

func NewAnalyzeHandler(analyzer *services.AnalyzerClient, maxUploadBytes int64, requestTimeout time.Duration) *AnalyzeHandler {
	return &AnalyzeHandler{
		analyzer:       analyzer,
		maxUploadBytes: maxUploadBytes,
		requestTimeout: requestTimeout,
	}
}

func (h *AnalyzeHandler) Handle(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxUploadBytes+multipartOverhead)

	if err := c.Request.ParseMultipartForm(h.maxUploadBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.RespondError(c, http.StatusRequestEntityTooLarge, "payload_too_large", h.sizeLimitMessage())
			return
		}
		httpx.RespondError(c, http.StatusBadRequest, "invalid_request", "The upload could not be parsed.")
		return
	}

	jobOffer := strings.TrimSpace(c.PostForm("jobOffer"))
	if len(jobOffer) < minJobOfferLength {
		httpx.RespondError(c, http.StatusBadRequest, "invalid_job_offer", "The job posting is too short to analyze.")
		return
	}
	jobTitle := strings.TrimSpace(c.PostForm("jobTitle"))

	header, err := c.FormFile("cv")
	if err != nil {
		httpx.RespondError(c, http.StatusBadRequest, "cv_required", "A CV file is required.")
		return
	}
	if header.Size > h.maxUploadBytes {
		httpx.RespondError(c, http.StatusRequestEntityTooLarge, "payload_too_large", h.sizeLimitMessage())
		return
	}
	if !isPDF(header) {
		httpx.RespondError(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "Only PDF files are supported.")
		return
	}

	file, err := header.Open()
	if err != nil {
		httpx.RespondError(c, http.StatusInternalServerError, "upload_error", "The CV file could not be read.")
		return
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	analysis, err := h.analyzer.Analyze(ctx, services.AnalyzeRequest{
		CV:       file,
		Filename: header.Filename,
		JobOffer: jobOffer,
		JobTitle: jobTitle,
	})
	if err != nil {
		h.respondUpstream(c, err)
		return
	}

	c.JSON(http.StatusOK, transport.AnalysisResponse{
		Analysis:   *analysis,
		CVFilename: header.Filename,
	})
}

// respondUpstream translates a failure from the AI service into a client-facing
// error: the caller's own 4xx mistakes are passed through, while our failures
// (timeouts, network errors, 5xx) collapse into a 502/504.
func (h *AnalyzeHandler) respondUpstream(c *gin.Context, err error) {
	var upstream *services.UpstreamError
	if errors.As(err, &upstream) && upstream.IsClientError() {
		httpx.RespondError(c, upstream.StatusCode, upstream.Code, upstream.Message)
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		httpx.RespondError(c, http.StatusGatewayTimeout, "upstream_timeout", "The analysis took too long. Please try again.")
		return
	}

	log.Printf("analyze: upstream failure: %v", err)
	httpx.RespondError(c, http.StatusBadGateway, "upstream_unavailable", "The analysis service is temporarily unavailable.")
}

func (h *AnalyzeHandler) sizeLimitMessage() string {
	return fmt.Sprintf("CV exceeds the %d MB limit.", h.maxUploadBytes/(1024*1024))
}

func isPDF(header *multipart.FileHeader) bool {
	if strings.EqualFold(header.Header.Get("Content-Type"), "application/pdf") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(header.Filename), ".pdf")
}
