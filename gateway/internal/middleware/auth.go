package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/papyrus/gateway/internal/httpx"
)

// ContextUserID is the gin context key under which the authenticated user's id
// is stored for downstream handlers and the rate limiter.
const ContextUserID = "userID"

// Supabase signs access tokens with asymmetric keys. Restricting the accepted
// algorithms to those avoids algorithm-confusion attacks; the verification key
// itself is resolved from the project's JWKS by the supplied keyfunc.
var allowedAlgorithms = []string{"ES256", "RS256"}

func Auth(keyfunc jwt.Keyfunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			httpx.RespondError(c, http.StatusUnauthorized, "unauthorized", "A valid access token is required.")
			return
		}

		claims := jwt.MapClaims{}
		parsed, err := jwt.ParseWithClaims(token, claims, keyfunc, jwt.WithValidMethods(allowedAlgorithms))
		if err != nil || !parsed.Valid {
			httpx.RespondError(c, http.StatusUnauthorized, "unauthorized", "Your session is invalid or has expired.")
			return
		}

		subject, _ := claims["sub"].(string)
		if subject == "" {
			httpx.RespondError(c, http.StatusUnauthorized, "unauthorized", "The access token is missing a subject.")
			return
		}

		c.Set(ContextUserID, subject)
		c.Next()
	}
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):]), true
	}
	return "", false
}
