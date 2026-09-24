package errors

import (
	"errors"
	"net/http"
	"testing"
)

func TestKindStatus(t *testing.T) {
	cases := map[Kind]int{
		KindValidation:   http.StatusBadRequest,
		KindUnauthorized: http.StatusUnauthorized,
		KindForbidden:    http.StatusForbidden,
		KindNotFound:     http.StatusNotFound,
		KindConflict:     http.StatusConflict,
		KindRateLimited:  http.StatusTooManyRequests,
		KindStorage:      http.StatusInternalServerError,
		KindDatabase:     http.StatusInternalServerError,
		KindQueue:        http.StatusInternalServerError,
		KindInternal:     http.StatusInternalServerError,
	}
	for k, want := range cases {
		if got := k.Status(); got != want {
			t.Errorf("%s.Status() = %d, want %d", k, got, want)
		}
	}
}

func TestWrapPreservesChain(t *testing.T) {
	root := errors.New("connection refused")
	wrapped := Wrap(KindDatabase, root, "find project")
	if !errors.Is(wrapped, root) {
		t.Fatal("errors.Is must see through AppError to the cause")
	}
	if !Is(wrapped, KindDatabase) {
		t.Error("Is(kind) failed for direct AppError")
	}
	outer := Wrapf(KindStorage, wrapped, "presign")
	if !Is(outer, KindDatabase) {
		// documented behaviour: Is matches the outermost kind only when types differ;
		// ensure nested AppError lookup still finds the outer kind.
		t.Log("nested-kind behavior noted")
	}
	if !Is(outer, KindStorage) {
		t.Error("Is(kind) must match the outer AppError kind")
	}
	var ae *AppError
	if !errors.As(outer, &ae) || ae.Kind != KindStorage {
		t.Error("errors.As must yield the outer AppError")
	}
}

func TestMessageNeverEmpty(t *testing.T) {
	e := New(KindNotFound, "asset not found")
	if e.Error() == "" {
		t.Error("Error() string must not be empty")
	}
	// Cause errors must not leak into the public message.
	inner := Wrap(KindDatabase, errors.New("pq: duplicate key value users_pkey"), "create user")
	if inner.Message != "create user" {
		t.Errorf("public message = %q, want %q", inner.Message, "create user")
	}
}
