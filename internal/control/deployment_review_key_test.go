package control

import (
	"errors"
	"testing"
)

type failingEntropy struct{ calls int }

func (r *failingEntropy) Read([]byte) (int, error) {
	r.calls++
	return 0, errors.New("entropy unavailable")
}
func TestReviewRevisionFailsClosedWhenEntropyFails(t *testing.T) {
	reader := &failingEntropy{}
	key := deploymentReviewKey{entropy: reader}
	for i := 0; i < 2; i++ {
		value, err := key.sign([]byte("secret"))
		if err == nil || value != "" {
			t.Fatal("entropy failure produced revision")
		}
	}
	if reader.calls != 1 {
		t.Fatal("entropy failure was not cached")
	}
}
