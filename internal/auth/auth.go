// Package auth issues and validates JWTs and hashes passwords.
//
// Tokens are stateless (HS256). Passwords use scrypt from x/crypto, which is
// memory-hard and therefore a better default than a bare SHA/SHA-256 chain.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/scrypt"
)

// Errors surfaced to the API layer.
var (
	ErrInvalidToken = errors.New("invalid or expired token")
	ErrBadPassword  = errors.New("password does not match")
)

// Claims is the JWT payload.
type Claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"usr"`
	Email    string `json:"email"`
	Plan     string `json:"plan"`
	jwt.RegisteredClaims
}

// Issuer mints and verifies tokens.
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

// NewIssuer builds a token issuer. A short secret is rejected to avoid
// accepting a trivially forgeable default.
func NewIssuer(secret string, ttl time.Duration) *Issuer {
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	return &Issuer{secret: []byte(secret), ttl: ttl}
}

// TTL is the lifetime of freshly issued tokens.
func (i *Issuer) TTL() time.Duration { return i.ttl }

// Token mints a signed token for a user.
func (i *Issuer) Token(userID int64, username, email, plan string) (string, time.Time, error) {
	now := time.Now().UTC()
	exp := now.Add(i.ttl)
	claims := Claims{
		UserID:   userID,
		Username: username,
		Email:    email,
		Plan:     plan,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(userID, 10),
			Issuer:    "bunkr-downloader",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        randomID(),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

// Parse validates a token and returns its claims.
func (i *Issuer) Parse(raw string) (*Claims, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrInvalidToken
	}
	var claims Claims
	_, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return i.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer("bunkr-downloader"))
	if err != nil {
		return nil, ErrInvalidToken
	}
	if claims.UserID <= 0 {
		return nil, ErrInvalidToken
	}
	return &claims, nil
}

// BearerToken extracts the token from an Authorization header value.
func BearerToken(header string) string {
	const prefix = "bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}

// ------------------------------------------------------------- password hash

const (
	scryptN = 1 << 15 // 32 MiB
	scryptR = 8
	scryptP = 1
	keyLen  = 32
	saltLen = 16
)

// HashPassword returns "scrypt$N$r$p$salt$key" using base64 (raw URL) encoding.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	key, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return "", fmt.Errorf("scrypt: %w", err)
	}
	enc := base64.RawURLEncoding
	return fmt.Sprintf("scrypt$%d$%d$%d$%s$%s", scryptN, scryptR, scryptP,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassword checks a plaintext password against a stored hash in
// constant time.
func VerifyPassword(password, stored string) error {
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[0] != "scrypt" {
		return ErrBadPassword
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return ErrBadPassword
	}
	r, err := strconv.Atoi(parts[2])
	if err != nil {
		return ErrBadPassword
	}
	p, err := strconv.Atoi(parts[3])
	if err != nil {
		return ErrBadPassword
	}
	enc := base64.RawURLEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return ErrBadPassword
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil {
		return ErrBadPassword
	}
	got, err := scrypt.Key([]byte(password), salt, n, r, p, len(want))
	if err != nil {
		return ErrBadPassword
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrBadPassword
	}
	return nil
}

func randomID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
