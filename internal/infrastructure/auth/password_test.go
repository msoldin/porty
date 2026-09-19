package auth

import "testing"

func TestPasswordHashVerifiesCorrectPasswordAndRejectsWrongPassword(t *testing.T) {
	hasher := NewPasswordHasher()
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "correct horse battery staple" {
		t.Fatal("password was stored in plaintext")
	}
	if !hasher.Verify(encoded, "correct horse battery staple") {
		t.Fatal("correct password rejected")
	}
	if hasher.Verify(encoded, "wrong password") {
		t.Fatal("wrong password accepted")
	}
}
