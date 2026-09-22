package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/buildinfo"
)

// Health answers the platform's liveness check, and says which build answered.
//
// The revision is here rather than on a separate endpoint because the question
// "is my fix live yet?" is asked of whatever is already reachable, usually in
// the middle of diagnosing something else.
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"revision": buildinfo.Revision(),
	})
}
