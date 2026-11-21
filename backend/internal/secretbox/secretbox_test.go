package secretbox

import "testing"

func TestRoundTrip(t *testing.T) {
	token, err := Encrypt("session-secret-value", "p@ssw0rd-中文")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || token == "p@ssw0rd-中文" {
		t.Fatalf("token not encrypted: %q", token)
	}
	got, err := Decrypt("session-secret-value", token)
	if err != nil {
		t.Fatal(err)
	}
	if got != "p@ssw0rd-中文" {
		t.Fatalf("got %q", got)
	}
	if _, err := Decrypt("another-secret", token); err == nil {
		t.Fatal("wrong key should fail")
	}
	if s, err := Encrypt("x", ""); err != nil || s != "" {
		t.Fatalf("empty: %q %v", s, err)
	}
	if s, err := Decrypt("x", ""); err != nil || s != "" {
		t.Fatalf("empty decrypt: %q %v", s, err)
	}
}
