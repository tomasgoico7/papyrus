// Package httpx contains small HTTP helpers shared across handlers and
// middleware, most importantly a single error envelope so every failure the
// gateway emits has the same shape.
package httpx

import "github.com/gin-gonic/gin"

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

// RespondError writes a consistent error envelope and aborts the request so no
// downstream handler runs after a failure has been reported.
func RespondError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorEnvelope{Error: apiError{Code: code, Message: message}})
}
