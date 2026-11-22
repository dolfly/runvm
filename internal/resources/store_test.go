package resources

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "res"), 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestWriteReadRoundTrip(t *testing.T) {
	s := newStore(t)
	m, err := s.Write("hello.txt", []byte("hello world"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if m.URI != "runvm://files/hello.txt" {
		t.Errorf("URI = %q", m.URI)
	}
	if m.Size != 11 {
		t.Errorf("Size = %d, want 11", m.Size)
	}
	if m.Sha256 == "" {
		t.Error("Sha256 empty")
	}
	got, err := s.Read("hello.txt")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("Read = %q", got)
	}
}

func TestWriteOverwriteReflectsNewContent(t *testing.T) {
	s := newStore(t)
	if _, err := s.Write("f.txt", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write("f.txt", []byte("second")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read("f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Errorf("after overwrite Read = %q, want second", got)
	}
}

func TestUploadListRead(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(src, []byte{0x00, 0x01, 0x02, 0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := s.Upload(src, "sub/payload.bin")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if m.URI != "runvm://files/sub/payload.bin" {
		t.Errorf("URI = %q", m.URI)
	}
	if m.Size != 4 {
		t.Errorf("Size = %d, want 4", m.Size)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}
	if list[0].URI != "runvm://files/sub/payload.bin" {
		t.Errorf("listed URI = %q", list[0].URI)
	}
	if list[0].Size != 4 {
		t.Errorf("listed Size = %d", list[0].Size)
	}
	if list[0].ModTime.IsZero() {
		t.Error("listed ModTime zero")
	}

	got, err := s.Read("sub/payload.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x00, 0x01, 0x02, 0xff}) {
		t.Errorf("Read = %v, want binary round-trip", got)
	}
}

func TestEmptyFile(t *testing.T) {
	s := newStore(t)
	m, err := s.Write("empty.txt", nil)
	if err != nil {
		t.Fatalf("Write empty: %v", err)
	}
	if m.Size != 0 {
		t.Errorf("Size = %d, want 0", m.Size)
	}
	got, err := s.Read("empty.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Read = %v, want empty", got)
	}
	if !IsText(got) {
		t.Error("empty file should read as text")
	}
}

func TestTraversalRejected(t *testing.T) {
	s := newStore(t)
	traversal := []string{
		"../escape.txt",
		"sub/../../escape.txt",
		"..",
		"/etc/passwd",
	}
	for _, name := range traversal {
		if _, err := s.Write(name, []byte("x")); !errors.Is(err, ErrTraversal) {
			t.Errorf("Write(%q) err = %v, want ErrTraversal", name, err)
		}
		if _, err := s.Read(name); err == nil {
			t.Errorf("Read(%q) = nil error, want rejection", name)
		}
	}
	// Empty and whitespace-only paths are rejected too (their own error, not
	// traversal).
	for _, name := range []string{"", "   "} {
		if _, err := s.Write(name, []byte("x")); err == nil {
			t.Errorf("Write(%q) = nil error, want rejection", name)
		}
		if _, err := s.Read(name); err == nil {
			t.Errorf("Read(%q) = nil error, want rejection", name)
		}
	}
}

func TestSymlinkEscapeRejected(t *testing.T) {
	s := newStore(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A symlink inside the root pointing at an outside directory.
	link := filepath.Join(s.root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	// Writing through it must be refused, not followed.
	if _, err := s.Write("link/secret.txt", []byte("x")); !errors.Is(err, ErrTraversal) {
		t.Errorf("Write through symlink dir err = %v, want ErrTraversal", err)
	}
	// Reading through it must also be refused.
	if _, err := s.Read("link/secret.txt"); !errors.Is(err, ErrTraversal) {
		t.Errorf("Read through symlink dir err = %v, want ErrTraversal", err)
	}
	// The outside file must be untouched.
	got, err := os.ReadFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "secret" {
		t.Errorf("outside file changed: %q", got)
	}

	// A symlink file target pointing outside.
	fileLink := filepath.Join(s.root, "f.txt")
	if err := os.Symlink(secret, fileLink); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write("f.txt", []byte("x")); !errors.Is(err, ErrTraversal) {
		t.Errorf("Write to symlink target err = %v, want ErrTraversal", err)
	}

	// Listing must skip the symlinks, not follow them.
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("List = %d entries, want 0 (symlinks skipped): %v", len(list), list)
	}
}

func TestSizeLimitRejected(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "res"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write("big.txt", []byte("12345")); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Write oversized err = %v, want ErrTooLarge", err)
	}
	// Exactly the limit is fine.
	if _, err := s.Write("ok.txt", []byte("1234")); err != nil {
		t.Errorf("Write at limit: %v", err)
	}
	// Upload path enforces the limit too.
	dir := t.TempDir()
	src := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(src, []byte("1234567890"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upload(src, ""); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Upload oversized err = %v, want ErrTooLarge", err)
	}
}

func TestListSkipsTempFiles(t *testing.T) {
	s := newStore(t)
	if _, err := s.Write("real.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
	// A stray in-flight upload temp file must not surface as a resource.
	if err := os.WriteFile(filepath.Join(s.root, tempPrefix+"123"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "real.txt" {
		t.Errorf("List = %v, want only real.txt", list)
	}
}

func TestPermissions(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "res"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write("sub/a.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
	rootInfo, err := os.Stat(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if rootInfo.Mode().Perm() != 0o700 {
		t.Errorf("root mode = %o, want 700", rootInfo.Mode().Perm())
	}
	subInfo, err := os.Stat(filepath.Join(s.root, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if subInfo.Mode().Perm() != 0o700 {
		t.Errorf("subdir mode = %o, want 700", subInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(filepath.Join(s.root, "sub", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 600", fileInfo.Mode().Perm())
	}
}

func TestIsText(t *testing.T) {
	cases := []struct {
		data []byte
		want bool
	}{
		{[]byte("hello"), true},
		{[]byte("中文"), true},
		{nil, true},
		{[]byte{0x00}, false},
		{[]byte{0xff, 0xfe}, false}, // invalid UTF-8
		{[]byte("a\x00b"), false},
	}
	for _, c := range cases {
		if got := IsText(c.data); got != c.want {
			t.Errorf("IsText(%q) = %v, want %v", c.data, got, c.want)
		}
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"67108864": 64 << 20,
		"64MiB":    64 << 20,
		"64MIB":    64 << 20,
		"1KiB":     1 << 10,
		"1KB":      1 << 10,
		"2G":       2 << 30,
		" 16 MB ":  16 << 20,
		"512":      512,
	}
	for in, want := range cases {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "12XB", "-1"} {
		if _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) = nil error, want error", bad)
		}
	}
}

func TestDefaultDirEnvUnset(t *testing.T) {
	// DefaultDir is a bare relative name, not an absolute path: the caller
	// (main) joins it with the working directory, or uses it as-is.
	if strings.Contains(filepath.ToSlash(DefaultDir), "/") {
		t.Errorf("DefaultDir = %q, want a bare directory name", DefaultDir)
	}
}
