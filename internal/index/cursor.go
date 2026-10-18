package index

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

// ErrBadCursor: a cursor that this server did not produce.
var ErrBadCursor = errors.New("invalid cursor")

// Cursor is the position after the last returned row: its sort key and name.
type Cursor struct {
	Key  string `json:"k"`
	Name string `json:"n"`
}

// EncodeCursor returns the opaque form (base64url JSON).
func EncodeCursor(c Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor parses an opaque cursor.
func DecodeCursor(s string) (Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, ErrBadCursor
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil || c.Name == "" {
		return Cursor{}, ErrBadCursor
	}
	return c, nil
}
