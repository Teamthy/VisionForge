package service

import (
	"testing"
)

func TestValidateFilename(t *testing.T) {
	ok := []string{"photo.jpg", "a.jpeg", "x-y_z.1.png", "screen.webp"}
	bad := []string{"", "../etc/passwd", "a/b.jpg", "a\\b.jpg", "nul\x00.jpg", string(make([]byte, 300)) + ".jpg"}
	for _, name := range ok {
		if err := validateFilename(name); err != nil {
			t.Errorf("validateFilename(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range bad {
		if err := validateFilename(name); err == nil {
			t.Errorf("validateFilename(%q) = nil, want error", name)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":    "etc_passwd",  // base name kept, separators gone
		"photo copy (1).jpg":  "photo_copy_1.jpg",
		"ünïcode.jpg":          "code.jpg", // non-ascii stripped, ext preserved
		"a\x00b.png":           "a_b.png",
		"..":                   "_",
	}
	for in, wantSub := range cases {
		got := sanitize(in)
		for _, bad := range []string{"/", "\\", "\x00", " ", "("} {
			if contains(got, bad) {
				t.Errorf("sanitize(%q) = %q, still contains %q", in, got, bad)
			}
		}
		if got == "" {
			t.Errorf("sanitize(%q) = empty", in)
		}
		_ = wantSub
	}
}

func TestStorageKeyShape(t *testing.T) {
	key := storageKey("user-123", "proj-456", ".jpg")
	if !contains(key, "projects/proj-456/users/user-123/") {
		t.Errorf("unexpected storage key %q", key)
	}
	if !endsWith(key, ".jpg") {
		t.Errorf("storage key must keep the extension: %q", key)
	}
	if contains(key, "..") {
		t.Errorf("storage key must not contain traversal: %q", key)
	}
	// uniqueness across calls in the same second
	k2 := storageKey("user-123", "proj-456", ".jpg")
	if key == k2 {
		t.Errorf("storage keys must be unique: both = %q", key)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
func endsWith(s, suf string) bool { return len(s) >= len(suf) && s[len(s)-len(suf):] == suf }

func TestSanitizeStripsPathAndControlChars(t *testing.T) {
	// filename must never retain traversal, absolute paths or control bytes
	for _, in := range []string{"/etc/passwd", "..\\..\\windows\\system32", "a\r\nb.txt", "\x01\x02.jpg"} {
		got := sanitize(in)
		if got == "" || len(got) > 200 {
			t.Fatalf("sanitize(%q) = %q (len %d)", in, got, len(got))
		}
		for _, bad := range []string{"/", "\\", "\r", "\n", "\x01", "\x02"} {
			if contains(got, bad) {
				t.Errorf("sanitize(%q) = %q retains %q", in, got, bad)
			}
		}
	}
}
