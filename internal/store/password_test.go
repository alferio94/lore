package store

import "testing"

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == "" || hash == "correct horse battery staple" {
		t.Fatalf("HashPassword() returned unsafe hash %q", hash)
	}

	valid, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil {
		t.Fatalf("VerifyPassword(valid) error = %v", err)
	}
	if !valid {
		t.Fatalf("VerifyPassword(valid) = false, want true")
	}

	valid, err = VerifyPassword("wrong password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword(invalid) error = %v", err)
	}
	if valid {
		t.Fatalf("VerifyPassword(invalid) = true, want false")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	valid, err := VerifyPassword("password", "not-a-bcrypt-hash")
	if err == nil {
		t.Fatalf("VerifyPassword(malformed) error = nil, want error")
	}
	if valid {
		t.Fatalf("VerifyPassword(malformed) = true, want false")
	}
}
