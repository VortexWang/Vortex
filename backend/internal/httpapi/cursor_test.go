package httpapi_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vortex/backend/internal/httpapi"
)

func TestCursorBindsPrincipalScopeCollectionSortAndExpiry(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	c, err := httpapi.NewCursorCodec([]byte(strings.Repeat("s", 32)), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	scope := httpapi.CursorScope{PrincipalID: "actor", TenantID: "tenant", Collection: "rules", Direction: "forward", Filter: "enabled=true"}
	position := httpapi.CursorPosition{CreatedAt: now.Add(-time.Minute), ID: "record"}
	encoded, err := c.Encode(scope, position, now)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := c.Decode(encoded, scope, now)
	if err != nil || actual != position {
		t.Fatal(actual, err)
	}
	for _, field := range []string{"principal", "tenant", "collection", "direction", "filter"} {
		t.Run(field, func(t *testing.T) {
			changed := scope
			switch field {
			case "principal":
				changed.PrincipalID = "other"
			case "tenant":
				changed.TenantID = "other"
			case "collection":
				changed.Collection = "nodes"
			case "direction":
				changed.Direction = "backward"
			case "filter":
				changed.Filter = "enabled=false"
			}
			if _, err := c.Decode(encoded, changed, now); !errors.Is(err, httpapi.ErrCursor) {
				t.Fatal(err)
			}
		})
	}
	if _, err = c.Decode(encoded, scope, now.Add(time.Hour)); !errors.Is(err, httpapi.ErrCursor) {
		t.Fatal("expired cursor", err)
	}
	if _, err = c.Decode(encoded, scope, now.Add(-time.Second)); !errors.Is(err, httpapi.ErrCursor) {
		t.Fatal("future cursor", err)
	}
	other, _ := httpapi.NewCursorCodec([]byte(strings.Repeat("t", 32)), time.Hour)
	if _, err = other.Decode(encoded, scope, now); !errors.Is(err, httpapi.ErrCursor) {
		t.Fatal("wrong key", err)
	}
}
func TestCursorRejectsMalformedNonCanonicalAndTamperedTokens(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	c, _ := httpapi.NewCursorCodec([]byte(strings.Repeat("s", 32)), time.Hour)
	scope := httpapi.CursorScope{PrincipalID: "actor", TenantID: "tenant", Collection: "rules", Direction: "forward"}
	valid, err := c.Encode(scope, httpapi.CursorPosition{CreatedAt: now, ID: "record"}, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", valid + "=", strings.Repeat("a", 4097), "a.b.c", "..", valid[:len(valid)-2] + "AA", " " + valid, valid + "\n"} {
		if _, err = c.Decode(token, scope, now); !errors.Is(err, httpapi.ErrCursor) {
			t.Fatal("invalid cursor accepted", token, err)
		}
	}
	for _, key := range [][]byte{nil, []byte("short")} {
		if _, err = httpapi.NewCursorCodec(key, time.Hour); !errors.Is(err, httpapi.ErrCursor) {
			t.Fatal("weak key", err)
		}
	}
	for _, ttl := range []time.Duration{0, -time.Second, 25 * time.Hour, 1500 * time.Millisecond} {
		if _, err = httpapi.NewCursorCodec([]byte(strings.Repeat("s", 32)), ttl); !errors.Is(err, httpapi.ErrCursor) {
			t.Fatal("invalid ttl", err)
		}
	}
}
func TestCursorUsesExactMicrosecondOrderingAndCopiesSigningKey(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	key := []byte(strings.Repeat("s", 32))
	c, _ := httpapi.NewCursorCodec(key, time.Hour)
	key[0] = 'x'
	c2, _ := httpapi.NewCursorCodec([]byte(strings.Repeat("s", 32)), time.Hour)
	scope := httpapi.CursorScope{PrincipalID: "a", TenantID: "t", Collection: "rules", Direction: "forward"}
	position := httpapi.CursorPosition{CreatedAt: now.Add(-time.Microsecond), ID: "z"}
	token, err := c.Encode(scope, position, now)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := c2.Decode(token, scope, now)
	if err != nil || actual != position {
		t.Fatal(actual, err)
	}
	for _, bad := range []httpapi.CursorPosition{{CreatedAt: time.Time{}, ID: "z"}, {CreatedAt: now, ID: ""}, {CreatedAt: now, ID: strings.Repeat("a", 129)}, {CreatedAt: now.Add(time.Nanosecond), ID: "z"}} {
		if _, err = c.Encode(scope, bad, now); !errors.Is(err, httpapi.ErrCursor) {
			t.Fatal("noncanonical ordering tuple", err)
		}
	}
}
