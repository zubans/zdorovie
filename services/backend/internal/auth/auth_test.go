package auth

import (
	"testing"
	"time"
)

func TestJWTAndTokens(t *testing.T) {
	secret := "super_secret_test_key"
	jwtStr, err := IssueJWT(secret, 99, "client", "test@example.com", time.Hour)
	if err != nil {
		t.Fatalf("IssueJWT failed: %v", err)
	}
	claims, err := ParseJWT(secret, jwtStr)
	if err != nil {
		t.Fatalf("ParseJWT failed: %v", err)
	}
	if claims.UserID != 99 || claims.Role != "client" || claims.Email != "test@example.com" {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	code, otpHash, err := GenerateOTP()
	if err != nil || len(code) != 6 || otpHash != HashToken("otp:"+code) {
		t.Fatalf("OTP generation mismatch")
	}

	pwHash, err := HashPassword("my_pass_123")
	if err != nil || !CheckPassword(pwHash, "my_pass_123") || CheckPassword(pwHash, "wrong") {
		t.Fatalf("password hash verification failed")
	}
}
