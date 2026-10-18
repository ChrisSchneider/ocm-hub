package index

import (
	"errors"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{Key: "2026-01-01T00:00:00Z", Name: "acme.org/x"}
	got, err := DecodeCursor(EncodeCursor(c))
	if err != nil || got != c {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, bad := range []string{"!!", "e30", "bm90LWpzb24"} { // invalid b64, "{}", "not-json"
		if _, err := DecodeCursor(bad); !errors.Is(err, ErrBadCursor) {
			t.Errorf("DecodeCursor(%q) = %v", bad, err)
		}
	}
}
