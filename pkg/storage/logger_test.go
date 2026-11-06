package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunDirectoryAndLogMarkers(t *testing.T) {
	root := t.TempDir()
	when := time.Date(2026, 11, 6, 12, 34, 56, 0, time.Local)
	run, err := NewRun(root, "example-repo", "build", when)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(root, "example-repo", "build", "20261106_123456")
	if run.Directory != expected {
		t.Fatalf("directory %q, expected %q", run.Directory, expected)
	}
	log, err := run.OpenJob("run-tests")
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Request("cd /app/repo-workspace && pytest"); err != nil {
		t.Fatal(err)
	}
	if err := log.Response("3 passed"); err != nil {
		t.Fatal(err)
	}
	if err := log.Response(""); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(expected, "run-tests.log"))
	if err != nil {
		t.Fatal(err)
	}
	want := ">> cd /app/repo-workspace && pytest\n<< 3 passed\n<< \n"
	if string(content) != want {
		t.Fatalf("unexpected contents: %q", content)
	}
}

func TestDuplicateTimestampDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	when := time.Now()
	a, err := NewRun(root, "repo", "pipeline", when)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewRun(root, "repo", "pipeline", when)
	if err != nil {
		t.Fatal(err)
	}
	if a.Directory == b.Directory || !strings.HasSuffix(b.Directory, "_001") {
		t.Fatalf("unique directories expected, got %q and %q", a.Directory, b.Directory)
	}
}

func TestRejectTraversalAndLogClobber(t *testing.T) {
	for _, invalid := range []string{"../etc", "a/b", "..", ".hidden", "", " space"} {
		if err := ValidateName(invalid); err == nil {
			t.Errorf("accepted invalid name %q", invalid)
		}
	}
	run, err := NewRun(t.TempDir(), "repo", "pipeline", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	log, err := run.OpenJob("job")
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := run.OpenJob("job"); !os.IsExist(err) {
		t.Fatalf("expected file-exists error, got %v", err)
	}
}
