package secrets

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func testKey() []byte { return bytes.Repeat([]byte{7}, KeySize) }

func TestSealOpenRoundTrip(t *testing.T) {
	box, err := New(testKey())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal([]byte(`{"capi_token":"EAAB-secret"}`), "meta")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("EAAB-secret")) {
		t.Fatal("sealed value contains the plaintext")
	}
	plain, err := box.Open(sealed, "meta")
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != `{"capi_token":"EAAB-secret"}` {
		t.Errorf("got %q", plain)
	}

	// Two seals of the same value differ (fresh nonce each time).
	again, _ := box.Seal([]byte(`{"capi_token":"EAAB-secret"}`), "meta")
	if bytes.Equal(sealed, again) {
		t.Error("two seals of the same plaintext are identical")
	}
}

func TestOpenRefusesWrongContextKeyOrTampering(t *testing.T) {
	box, _ := New(testKey())
	sealed, _ := box.Seal([]byte("token"), "meta")

	if _, err := box.Open(sealed, "google"); !errors.Is(err, ErrCorrupt) {
		t.Errorf("other context: got %v, want ErrCorrupt", err)
	}

	other, _ := New(bytes.Repeat([]byte{9}, KeySize))
	if _, err := other.Open(sealed, "meta"); !errors.Is(err, ErrCorrupt) {
		t.Errorf("other key: got %v, want ErrCorrupt", err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := box.Open(tampered, "meta"); !errors.Is(err, ErrCorrupt) {
		t.Errorf("tampered: got %v, want ErrCorrupt", err)
	}

	if _, err := box.Open([]byte{1, 2}, "meta"); !errors.Is(err, ErrCorrupt) {
		t.Errorf("truncated: got %v, want ErrCorrupt", err)
	}
}

func TestNilBoxHasNoKey(t *testing.T) {
	box, err := New(nil)
	if err != nil || box != nil {
		t.Fatalf("New(nil) = %v, %v; want nil, nil", box, err)
	}
	if _, err := box.Seal([]byte("x"), "meta"); !errors.Is(err, ErrNoKey) {
		t.Errorf("Seal: got %v, want ErrNoKey", err)
	}
	if _, err := box.Open([]byte("x"), "meta"); !errors.Is(err, ErrNoKey) {
		t.Errorf("Open: got %v, want ErrNoKey", err)
	}
}

func TestParseKey(t *testing.T) {
	key := testKey()
	for name, raw := range map[string]string{
		"base64":     base64.StdEncoding.EncodeToString(key),
		"raw base64": base64.RawURLEncoding.EncodeToString(key),
		"hex":        hex.EncodeToString(key),
		"padded":     "  " + base64.StdEncoding.EncodeToString(key) + "\n",
	} {
		got, err := ParseKey(raw)
		if err != nil || !bytes.Equal(got, key) {
			t.Errorf("%s: got %x, %v", name, got, err)
		}
	}
	for _, bad := range []string{"", "short", base64.StdEncoding.EncodeToString(key[:16]), strings.Repeat("z", 64)} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) accepted", bad)
		}
	}
}

func TestMask(t *testing.T) {
	if got := Mask(""); got != "" {
		t.Errorf("Mask(\"\") = %q", got)
	}
	if got := Mask("short-secret"); strings.Contains(got, "cret") {
		t.Errorf("short secret leaked characters: %q", got)
	}
	got := Mask("EAABsbCS1iHgBAKZBZCq1234")
	if !strings.HasSuffix(got, "1234") || strings.Contains(got, "EAAB") {
		t.Errorf("Mask(long) = %q, want dots + last four", got)
	}
}
