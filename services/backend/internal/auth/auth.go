package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const SessionCookieName = "zdorovie_session"

// Claims encodes authenticated user identity in JWT.
type Claims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// GenerateToken returns a cryptographically random URL-safe token and its SHA-256 hex digest.
func GenerateToken(numBytes int) (raw string, hash string, err error) {
	if numBytes <= 0 {
		numBytes = 32
	}
	buf := make([]byte, numBytes)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	hash = HashToken(raw)
	return raw, hash, nil
}

// GenerateOTP returns a 6-digit numeric OTP code and its SHA-256 hex digest.
func GenerateOTP() (code string, hash string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", "", err
	}
	code = fmt.Sprintf("%06d", n.Int64())
	return code, HashToken("otp:" + code), nil
}

// HashOTP returns a SHA-256 hex digest of an OTP code scoped to a specific email address.
func HashOTP(email, code string) string {
	return HashToken("otp:" + strings.ToLower(strings.TrimSpace(email)) + ":" + strings.TrimSpace(code))
}

// GenerateOTPForEmail returns a 6-digit numeric OTP code and its email-scoped SHA-256 hex digest.
func GenerateOTPForEmail(email string) (code string, hash string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", "", err
	}
	code = fmt.Sprintf("%06d", n.Int64())
	return code, HashOTP(email, code), nil
}

// HashToken returns hex-encoded SHA-256 of a token string.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// HashPassword hashes a plaintext password with bcrypt.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword compares a bcrypt password hash against plaintext input.
func CheckPassword(hash, pw string) bool {
	if hash == "" || pw == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// IssueJWT signs a session JWT valid for ttl.
func IssueJWT(secret string, userID int64, role, email string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	now := time.Now().UTC()
	claims := Claims{
		UserID: userID,
		Role:   role,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(secret))
}

// ParseJWT validates and extracts Claims from a signed JWT string.
func ParseJWT(secret, tokenStr string) (*Claims, error) {
	if strings.TrimSpace(tokenStr) == "" {
		return nil, errors.New("empty token")
	}
	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid jwt claims")
	}
	return claims, nil
}

// ExtractBearerOrCookie reads token from Authorization: Bearer or session cookie.
func ExtractBearerOrCookie(r *http.Request) (token string, isBearer bool) {
	if hdr := strings.TrimSpace(r.Header.Get("Authorization")); hdr != "" {
		if strings.HasPrefix(strings.ToLower(hdr), "bearer ") {
			return strings.TrimSpace(hdr[7:]), true
		}
	}
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
		return c.Value, false
	}
	return "", false
}

// SetSessionCookie writes the HttpOnly session JWT cookie.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAgeSeconds int) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAgeSeconds,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie deletes the session cookie.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	SetSessionCookie(w, r, "", -1)
}
