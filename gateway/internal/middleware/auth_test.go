package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testKID = "test-key"

func newSigningKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	return key
}

func signES256(t *testing.T, key *ecdsa.PrivateKey, subject string, expiry time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"sub": subject,
		"aud": "authenticated",
		"exp": expiry.Unix(),
	})
	token.Header["kid"] = testKID

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return signed
}

func TestAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	signingKey := newSigningKey(t)
	otherKey := newSigningKey(t)

	// Resolve the verification key by kid, mirroring the real JWKS keyfunc.
	keyfunc := func(token *jwt.Token) (interface{}, error) {
		if token.Header["kid"] == testKID {
			return &signingKey.PublicKey, nil
		}
		return nil, errors.New("unknown kid")
	}

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "valid token is accepted",
			authHeader: "Bearer " + signES256(t, signingKey, "user-123", time.Now().Add(time.Hour)),
			wantStatus: http.StatusOK,
		},
		{
			name:       "expired token is rejected",
			authHeader: "Bearer " + signES256(t, signingKey, "user-123", time.Now().Add(-time.Hour)),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "token signed with another key is rejected",
			authHeader: "Bearer " + signES256(t, otherKey, "user-123", time.Now().Add(time.Hour)),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing header is rejected",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed header is rejected",
			authHeader: "Token abc",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(Auth(keyfunc))
			engine.GET("/", func(c *gin.Context) {
				if c.GetString(ContextUserID) == "" {
					t.Error("expected userID to be set on the context")
				}
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, rec.Code)
			}
		})
	}
}
