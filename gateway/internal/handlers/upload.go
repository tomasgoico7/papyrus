package handlers

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/httpx"
)

const (
	minJobOfferLength = 40
	multipartOverhead = 1 << 20 // headroom for job-posting text and multipart framing
)

// analysisUpload is a validated analyze request. The caller owns File and must
// close it.
type analysisUpload struct {
	File     multipart.File
	Filename string
	JobOffer string
	JobTitle string
}

// parseAnalysisUpload validates the multipart body that the synchronous and the
// asynchronous analyze endpoints share. It writes the error response itself and
// reports whether the caller should carry on, so the two endpoints cannot drift
// into accepting different things.
func parseAnalysisUpload(c *gin.Context, maxUploadBytes int64) (analysisUpload, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes+multipartOverhead)

	if err := c.Request.ParseMultipartForm(maxUploadBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.RespondError(c, http.StatusRequestEntityTooLarge, "payload_too_large", sizeLimitMessage(maxUploadBytes))
			return analysisUpload{}, false
		}
		httpx.RespondError(c, http.StatusBadRequest, "invalid_request", "The upload could not be parsed.")
		return analysisUpload{}, false
	}

	jobOffer := strings.TrimSpace(c.PostForm("jobOffer"))
	if len(jobOffer) < minJobOfferLength {
		httpx.RespondError(c, http.StatusBadRequest, "invalid_job_offer", "The job posting is too short to analyze.")
		return analysisUpload{}, false
	}

	header, err := c.FormFile("cv")
	if err != nil {
		httpx.RespondError(c, http.StatusBadRequest, "cv_required", "A CV file is required.")
		return analysisUpload{}, false
	}
	if header.Size > maxUploadBytes {
		httpx.RespondError(c, http.StatusRequestEntityTooLarge, "payload_too_large", sizeLimitMessage(maxUploadBytes))
		return analysisUpload{}, false
	}
	if !isPDF(header) {
		httpx.RespondError(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "Only PDF files are supported.")
		return analysisUpload{}, false
	}

	file, err := header.Open()
	if err != nil {
		httpx.RespondError(c, http.StatusInternalServerError, "upload_error", "The CV file could not be read.")
		return analysisUpload{}, false
	}

	return analysisUpload{
		File:     file,
		Filename: header.Filename,
		JobOffer: jobOffer,
		JobTitle: strings.TrimSpace(c.PostForm("jobTitle")),
	}, true
}

func sizeLimitMessage(maxUploadBytes int64) string {
	return fmt.Sprintf("CV exceeds the %d MB limit.", maxUploadBytes/(1024*1024))
}

func isPDF(header *multipart.FileHeader) bool {
	if strings.EqualFold(header.Header.Get("Content-Type"), "application/pdf") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(header.Filename), ".pdf")
}
