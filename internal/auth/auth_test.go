package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	const password = "correct horse battery staple"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "scrypt$") {
		t.Errorf("hash format = %q, want a scrypt hash", hash)
	}
	if strings.Contains(hash, password) {
		t.Error("hash leaks the plaintext password")
	}
	if err := VerifyPassword(password, hash); err != nil {
		t.Errorf("VerifyPassword(correct) = %v, want nil", err)
	}
	if err := VerifyPassword("wrong password", hash); err == nil {
		t.Error("VerifyPassword(wrong) = nil, want an error")
	}
	if err := VerifyPassword(password, "garbage"); err == nil {
		t.Error("VerifyPassword(garbage) = nil, want an error")
	}
	if err := VerifyPassword(password, "scrypt$abc$8$1$aa$bb"); err == nil {
		t.Error("VerifyPassword(malformed) = nil, want an error")
	}
}

func TestHashIsSalted(t *testing.T) {
	a, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two hashes of the same password are identical; salt is not random")
	}
}

func TestTokenRoundTrip(t *testing.T) {
	iss := NewIssuer("test-secret-value-long-enough", time.Hour)
	tok, exp, err := iss.Token(42, "alice", "alice@test.local", "free")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok == "" {
		t.Fatal("Token returned an empty string")
	}
	if !exp.After(time.Now()) {
		t.Errorf("expiry %v is not in the future", exp)
	}

	claims, err := iss.Parse(tok)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.UserID != 42 || claims.Username != "alice" || claims.Plan != "free" {
		t.Errorf("claims = %+v, want uid 42 / alice / free", claims)
	}
}

func TestTokenRejectsTampering(t *testing.T) {
	iss := NewIssuer("secret-a", time.Hour)
	other := NewIssuer("secret-b", time.Hour)
	tok, _, err := iss.Token(1, "a", "a@b.c", "free")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Parse(tok); err == nil {
		t.Error("a token signed with a different secret was accepted")
	}
	if _, err := iss.Parse(tok + "x"); err == nil {
		t.Error("a tampered token was accepted")
	}
	if _, err := iss.Parse(""); err == nil {
		t.Error("an empty token was accepted")
	}
	if _, err := iss.Parse("not.a.jwt"); err == nil {
		t.Error("a garbage token was accepted")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	// A non-positive TTL is clamped to the default, so mint a very short one
	// and wait for it to lapse.
	iss := NewIssuer("secret", 10*time.Millisecond)
	tok, _, err := iss.Token(1, "a", "a@b.c", "free")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := iss.Parse(tok); err == nil {
		t.Error("an expired token was accepted")
	}
}

func TestIssuerClampsNonPositiveTTL(t *testing.T) {
	iss := NewIssuer("secret", 0)
	if iss.TTL() <= 0 {
		t.Error("a zero TTL was not replaced by the default")
	}
}

func TestBearerToken(t *testing.T) {
	cases := map[string]string{
		"Bearer abc.def.ghi": "abc.def.ghi",
		"bearer abc.def.ghi": "abc.def.ghi",
		"BEARER  spaced  ":   "spaced",
		"Basic xyz":          "",
		"":                   "",
		"Bearer":             "",
	}
	for header, want := range cases {
		if got := BearerToken(header); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", header, got, want)
		}
	}
}
