package draft

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenRejectsMissingFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "absent.md")); err == nil {
		t.Fatal("expected an error for a draft that does not exist")
	}
}

func TestOpenRejectsDirectory(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("expected an error for a draft that is not a regular file")
	}
}

func TestRereadReportsEditedContent(t *testing.T) {
	path := writeDraft(t, "hey team\n")
	handed, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := os.WriteFile(path, []byte("hey team, shipping today\n"), 0o600); err != nil {
		t.Fatalf("rewrite draft: %v", err)
	}

	text, edited, err := handed.Reread()
	if err != nil {
		t.Fatalf("Reread: %v", err)
	}
	if !edited {
		t.Error("expected edited=true after the content changed")
	}
	if want := "hey team, shipping today\n"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}

// A save in nvim rewrites the file even when the text is identical, so a fresh
// modification time must not read as an edit.
func TestRereadIgnoresTouchedModificationTime(t *testing.T) {
	path := writeDraft(t, "hey team\n")
	handed, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := os.WriteFile(path, []byte("hey team\n"), 0o600); err != nil {
		t.Fatalf("rewrite draft: %v", err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("touch draft: %v", err)
	}

	_, edited, err := handed.Reread()
	if err != nil {
		t.Fatalf("Reread: %v", err)
	}
	if edited {
		t.Error("expected edited=false for byte-identical content")
	}
}

func TestPathIsAbsolute(t *testing.T) {
	handed, err := Open(writeDraft(t, "hey team\n"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !filepath.IsAbs(handed.Path()) {
		t.Errorf("Path() = %q, want an absolute path", handed.Path())
	}
}

func writeDraft(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "draft.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write draft: %v", err)
	}
	return path
}
