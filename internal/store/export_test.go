package store

import "github.com/chaunceyxie1/BunkrDownloader/internal/auth"

// VerifyPasswordForTest re-exports the password verifier so the store tests can
// assert that what we persisted is a real scrypt hash of the original secret.
func VerifyPasswordForTest(hash, password string) error {
	return auth.VerifyPassword(password, hash)
}
