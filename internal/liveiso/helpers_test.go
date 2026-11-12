package liveiso

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckDestinationVacantFailsClosed(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "new.iso")
	if err := CheckDestinationVacant(empty); err != nil {
		t.Fatalf("unused destination rejected: %v", err)
	}
	if err := CheckDestinationVacant(""); err == nil {
		t.Fatal("empty destination accepted")
	}
	existing := filepath.Join(dir, "existing.iso")
	if err := os.WriteFile(existing, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckDestinationVacant(existing); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing destination accepted: %v", err)
	}
	link := filepath.Join(dir, "link.iso")
	if err := os.Symlink(empty, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckDestinationVacant(link); err == nil {
		t.Fatal("dangling symlink destination accepted")
	}
}

func TestRunToolReportsHostFailuresAndCancellation(t *testing.T) {
	bin := t.TempDir()
	script := filepath.Join(bin, "iso-tool")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nset -eu\ntest \"$1\" = ok\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := RunTool(context.Background(), script, "ok"); err != nil {
		t.Fatalf("host tool failed: %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho tool-failed >&2\nexit 29\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := RunTool(context.Background(), script); err == nil || !strings.Contains(err.Error(), "tool-failed") {
		t.Fatalf("lost host tool failure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RunTool(ctx, script); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not propagated: %v", err)
	}
}

func TestCopyFileDoesNotOverwriteStaging(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "kernel")
	destination := filepath.Join(dir, "kernel-copy")
	if err := os.WriteFile(source, []byte("kernel-content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(source, destination); err != nil {
		t.Fatalf("copy failed: %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != "kernel-content" {
		t.Fatalf("copy modified content: %q, %v", got, err)
	}
	if err := CopyFile(source, destination); err == nil {
		t.Fatal("existing destination overwritten")
	}
	if err := CopyFile(source+"-missing", filepath.Join(dir, "unused")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source accepted: %v", err)
	}
}
