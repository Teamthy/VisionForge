// Package auth handles password hashing, JWT minting/validation, and token utilities.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/visionforge/visionforge/packages/config"
	vtypes "github.com/visionforge/visionforge/packages/types"
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// Claims are the JWT claims used for access tokens.
type Claims struct {
	jwt.RegisteredClaims
	UserID string     `json:"uid"`
	Role   vtypes.Role `json:"role"`
	Type   string     `json:"typ"`
}

// Hasher hashes and verifies passwords.
type Hasher struct{ cost int }

// NewHasher returns a bcrypt-based password hasher.
func NewHasher(cost int) *Hasher {
	if cost < bcrypt.MinCost {
		cost = bcrypt.DefaultCost
	}
	return &Hasher{cost: cost}
}

// Hash returns the bcrypt hash of password.
func (h *Hasher) Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Verify returns nil if the password matches the hash.
func (h *Hasher) Verify(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// JWTManager mints and validates JWT access tokens.
type JWTManager struct {
	secret    []byte
	accessTTL time.Duration
}

// NewJWTManager creates a JWT manager.
func NewJWTManager(cfg config.AuthConfig) *JWTManager {
	return &JWTManager{secret: []byte(cfg.JWTSecret), accessTTL: cfg.JWTAccessTTL}
}

// MintAccessToken creates a signed access token for a user.
func (m *JWTManager) MintAccessToken(user *vtypes.User) (string, time.Time, error) {
	exp := time.Now().Add(m.accessTTL)
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(exp),
			Issuer:    "visionforge",
		},
		UserID: user.ID,
		Role:   user.Role,
		Type:   TokenTypeAccess,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(m.secret)
	return s, exp, err
}

// ParseAccessToken validates a token string and returns its claims.
func (m *JWTManager) ParseAccessToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	tok, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !tok.Valid || claims.Type != TokenTypeAccess {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// GenerateRefreshToken creates a high-entropy random refresh token and returns
// (token, sha256hex-hash).
func GenerateRefreshToken() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	tok := hex.EncodeToString(b)
	h := sha256.Sum256([]byte(tok))
	return tok, hex.EncodeToString(h[:]), nil
}

// HashRefreshToken returns the sha256 hex hash of a refresh token.
func HashRefreshToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// GenerateRequestID returns a random hex string suitable for request IDs.
func GenerateRequestID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return time.Now().Format("20060102T150405.000Z")
	}
	return hex.EncodeToString(b)
}
