package insulavm

import (
 "github.com/ahhvahh/InsulaVM/internal/guestfs"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A failing unmount must stop ISO publication, retain the staging tree,
// and avoid launching GRUB against an unresolved FUSE mount.
func TestExportLiveISOUnmountFailureRetainsStaging(t *testing.T) {
	privateTmp := t.TempDir()
	t.Setenv("TMPDIR", privateTmp)
	e, bin, destination := isoFailureFixture(t)
	grubLog := filepath.Join(t.TempDir(), "grub-was-called")
	t.Setenv("INSULA_TEST_GRUB_LOG", grubLog)
	isoToolScripts(t, bin, map[string]string{
		"guestunmount":  "echo guest-fuse-busy >&2; exit 71",
		"grub-mkrescue": "touch \"$INSULA_TEST_GRUB_LOG\"; exit 88",
	})

	exported, err := e.ExportProfileLiveISOTo(context.Background(), "isotest", destination)
	if exported != "" || !errors.Is(err, guestfs.ErrUnmount) {
		t.Fatalf("failed unmount accepted: iso=%q err=%v", exported, err)
	}
	if !strings.Contains(err.Error(), "guest-fuse-busy") || !strings.Contains(err.Error(), "staging retained at") {
		t.Fatalf("missing actionable FUSE error and retained path: %v", err)
	}
	if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("ISO was published after failed unmount: %v", statErr)
	}
	if _, statErr := os.Lstat(grubLog); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("GRUB must not run after failed unmount: %v", statErr)
	}
	stages, globErr := filepath.Glob(filepath.Join(privateTmp, "insula-live-iso-*"))
	if globErr != nil || len(stages) != 1 {
		t.Fatalf("unresolved FUSE staging removed: %v %v", stages, globErr)
	}
}

// An ISO may reach GRUB only after the QCOW2's guestmount has been released.
func TestExportLiveISOUnmountPrecedesGrubPublication(t *testing.T) {
	e, bin, destination := isoFailureFixture(t)
	marker := filepath.Join(t.TempDir(), "unmounted")
	t.Setenv("INSULA_TEST_UNMOUNT_MARKER", marker)
	isoToolScripts(t, bin, map[string]string{
		"guestunmount":  "printf unmounted > \"$INSULA_TEST_UNMOUNT_MARKER\"",
		"grub-mkrescue": "test -s \"$INSULA_TEST_UNMOUNT_MARKER\"; printf iso > \"$2\"",
	})
	exported, err := e.ExportProfileLiveISOTo(context.Background(), "isotest", destination)
	if err != nil || exported != destination {
		t.Fatalf("ISO was not published after safe unmount: iso=%q err=%v", exported, err)
	}
	if b, readErr := os.ReadFile(destination); readErr != nil || string(b) != "iso" {
		t.Fatalf("incorrect safely published ISO: %q %v", b, readErr)
	}
}

func TestGuestfsUnmountFailureNeverCleansPrivateKernel(t *testing.T) {
	bin := t.TempDir()
	isoToolScripts(t, bin, map[string]string{"guestunmount": "echo fuse-detach-error >&2; exit 99"})
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	called := false
	err := guestfs.UnmountReadOnly(t.TempDir(), func() { called = true })
	if !errors.Is(err, guestfs.ErrUnmount) || called || !strings.Contains(err.Error(), "fuse-detach-error") {
		t.Fatalf("unmount failure must preserve resources: cleanup=%t err=%v", called, err)
	}
}
