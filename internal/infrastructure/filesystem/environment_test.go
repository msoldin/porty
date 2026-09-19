package filesystem

import (
	"errors"
	"testing"
)

func TestSerializeEnvironmentIsDeterministicAndEscapesValues(t *testing.T) {
	got, err := SerializeEnvironment(map[string]string{
		"Z_LAST":  "line one\nline two",
		"A_FIRST": `quote " and slash \\`,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `A_FIRST="quote \" and slash \\\\"` + "\n" + `Z_LAST="line one\nline two"` + "\n"
	if string(got) != want {
		t.Fatalf("SerializeEnvironment() = %q, want %q", got, want)
	}
}

func TestSerializeEnvironmentRejectsInvalidKeyAndNUL(t *testing.T) {
	for _, values := range []map[string]string{{"BAD-KEY": "value"}, {"GOOD": "bad\x00value"}} {
		if _, err := SerializeEnvironment(values); !errors.Is(err, ErrInvalidEnvironment) {
			t.Fatalf("SerializeEnvironment(%#v) error = %v", values, err)
		}
	}
}
