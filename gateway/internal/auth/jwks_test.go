package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// A JWK rebuilt from an EC key should verify a token signed with that key,
// exercising the base64url coordinate decoding that the JWKS fetch relies on.
func TestECJWKVerifiesToken(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	encoded := jwk{
		Kty: "EC",
		Kid: "kid-1",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(key.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(key.Y.Bytes()),
	}

	public, err := encoded.publicKey()
	if err != nil {
		t.Fatalf("building public key from jwk: %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}

	parsed, err := jwt.Parse(
		signed,
		func(*jwt.Token) (interface{}, error) { return public, nil },
		jwt.WithValidMethods([]string{"ES256"}),
	)
	if err != nil || !parsed.Valid {
		t.Fatalf("expected token to verify with the parsed JWK, err=%v", err)
	}
}

func TestUnsupportedKeyTypeIsRejected(t *testing.T) {
	if _, err := (jwk{Kty: "oct", Kid: "x"}).publicKey(); err == nil {
		t.Fatal("expected an error for an unsupported key type")
	}
}
