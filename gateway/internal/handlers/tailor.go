package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/httpx"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
)

// The generate step sends back the extracted CV text plus answers as JSON, which
// is text-only and far smaller than an upload.
const maxTailorJSONBytes = 1 << 20 // 1 MB

type TailorHandler struct {
	tailor         *services.TailorClient
	maxUploadBytes int64
	requestTimeout time.Duration
}

func NewTailorHandler(tailor *services.TailorClient, maxUploadBytes int64, requestTimeout time.Duration) *TailorHandler {
	return &TailorHandler{
		tailor:         tailor,
		maxUploadBytes: maxUploadBytes,
		requestTimeout: requestTimeout,
	}
}

func (h *TailorHandler) Questions(c *gin.Context) {
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

	data, err := h.tailor.Questions(ctx, services.QuestionsRequest{
		CV:       file,
		Filename: header.Filename,
		JobOffer: jobOffer,
		JobTitle: strings.TrimSpace(c.PostForm("jobTitle")),
		Locale:   strings.TrimSpace(c.PostForm("locale")),
	})
	if err != nil {
		h.respondUpstream(c, err)
		return
	}

	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

func (h *TailorHandler) Generate(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTailorJSONBytes)

	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.RespondError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "The request is too large.")
			return
		}
		httpx.RespondError(c, http.StatusBadRequest, "invalid_request", "The request could not be read.")
		return
	}
	if len(payload) == 0 {
		httpx.RespondError(c, http.StatusBadRequest, "invalid_request", "The request body is empty.")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	data, err := h.tailor.Generate(ctx, payload)
	if err != nil {
		h.respondUpstream(c, err)
		return
	}

	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

func (h *TailorHandler) respondUpstream(c *gin.Context, err error) {
	var upstream *services.UpstreamError
	if errors.As(err, &upstream) && upstream.IsClientError() {
		httpx.RespondError(c, upstream.StatusCode, upstream.Code, upstream.Message)
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		httpx.RespondError(c, http.StatusGatewayTimeout, "upstream_timeout", "The request took too long. Please try again.")
		return
	}

	observability.LoggerFrom(c.Request.Context()).Error("tailor upstream failure", slog.Any("error", err))
	httpx.RespondError(c, http.StatusBadGateway, "upstream_unavailable", "The CV service is temporarily unavailable.")
}

func (h *TailorHandler) sizeLimitMessage() string {
	return fmt.Sprintf("CV exceeds the %d MB limit.", h.maxUploadBytes/(1024*1024))
}
