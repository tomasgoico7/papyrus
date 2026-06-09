package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type KeySet struct {
	url        string
	http       *http.Client
	minRefresh time.Duration

	mu        sync.RWMutex
	keys      map[string]crypto.PublicKey
	fetchedAt time.Time
}

func NewKeySet(jwksURL string) *KeySet {
	return &KeySet{
		url:        jwksURL,
		http:       &http.Client{Timeout: 10 * time.Second},
		minRefresh: 5 * time.Minute,
		keys:       make(map[string]crypto.PublicKey),
	}
}

func (ks *KeySet) Keyfunc(token *jwt.Token) (interface{}, error) {
	kid, ok := token.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, errors.New("token is missing a key id")
	}

	if key := ks.lookup(kid); key != nil {
		return key, nil
	}
	if err := ks.refresh(); err != nil {
		return nil, err
	}
	if key := ks.lookup(kid); key != nil {
		return key, nil
	}
	return nil, fmt.Errorf("no signing key for kid %q", kid)
}

func (ks *KeySet) lookup(kid string) crypto.PublicKey {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return ks.keys[kid]
}

func (ks *KeySet) refresh() error {
	ks.mu.RLock()
	fresh := len(ks.keys) > 0 && time.Since(ks.fetchedAt) < ks.minRefresh
	ks.mu.RUnlock()
	if fresh {
		return nil
	}

	keys, err := ks.fetch()
	if err != nil {
		return err
	}

	ks.mu.Lock()
	ks.keys = keys
	ks.fetchedAt = time.Now()
	ks.mu.Unlock()
	return nil
}

func (ks *KeySet) fetch() (map[string]crypto.PublicKey, error) {
	resp, err := ks.http.Get(ks.url)
	if err != nil {
		return nil, fmt.Errorf("fetching jwks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks endpoint returned status %d", resp.StatusCode)
	}

	var document struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
		return nil, fmt.Errorf("decoding jwks: %w", err)
	}

	keys := make(map[string]crypto.PublicKey, len(document.Keys))
	for _, key := range document.Keys {
		if key.Kid == "" {
			continue
		}
		if public, err := key.publicKey(); err == nil {
			keys[key.Kid] = public
		}
	}

	if len(keys) == 0 {
		return nil, errors.New("jwks contained no usable keys")
	}
	return keys, nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (k jwk) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "EC":
		return k.ecPublicKey()
	case "RSA":
		return k.rsaPublicKey()
	default:
		return nil, fmt.Errorf("unsupported key type %q", k.Kty)
	}
}

func (k jwk) ecPublicKey() (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported curve %q", k.Crv)
	}

	x, err := decodeBigInt(k.X)
	if err != nil {
		return nil, err
	}
	y, err := decodeBigInt(k.Y)
	if err != nil {
		return nil, err
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

func (k jwk) rsaPublicKey() (*rsa.PublicKey, error) {
	n, err := decodeBigInt(k.N)
	if err != nil {
		return nil, err
	}
	exponentBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decoding rsa exponent: %w", err)
	}

	exponent := 0
	for _, b := range exponentBytes {
		exponent = exponent<<8 | int(b)
	}
	return &rsa.PublicKey{N: n, E: exponent}, nil
}

func decodeBigInt(value string) (*big.Int, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decoding base64url: %w", err)
	}
	return new(big.Int).SetBytes(decoded), nil
}
