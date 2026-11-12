package guestfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guestfsStub(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestGuestfsMountKeepsNormalHostPath(t *testing.T) {
	bin := t.TempDir()
	guestfsStub(t, bin, "guestmount", `test "$1" = --ro; test "$2" = -a; test "$4" = -i`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cleanup, err := MountReadOnly(context.Background(), "/tmp/guest.qcow2", t.TempDir())
	if err != nil || cleanup == nil {
		t.Fatalf("normal read-only mount: cleanup=%v err=%v", cleanup != nil, err)
	}
	cleanup()
}

func TestGuestfsMountRecoversMissingKernelWithoutHostInstall(t *testing.T) {
	bin := t.TempDir()
	guestfsStub(t, bin, "guestmount", `
if [ -n "${SUPERMIN_KERNEL:-}" ] && [ -r "$SUPERMIN_KERNEL" ] && [ -d "${SUPERMIN_MODULES:-/missing}" ]; then
  echo "$SUPERMIN_KERNEL" > "$INSULA_GUESTFS_KERNEL_LOG"
  exit 0
fi
if [ "${LIBGUESTFS_DEBUG:-}" = 1 ]; then
  echo 'supermin: failed to find a suitable kernel (host_cpu=x86_64).' >&2
fi
echo 'libguestfs: error: /usr/bin/supermin exited with error status 1.' >&2
exit 1`)
	guestfsExtractedKernelStubs(t, bin)
	log := filepath.Join(t.TempDir(), "kernel-path")
	t.Setenv("INSULA_GUESTFS_KERNEL_LOG", log)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SUPERMIN_KERNEL", "")
	t.Setenv("SUPERMIN_MODULES", "")
	cleanup, err := MountReadOnly(context.Background(), "/tmp/guest.qcow2", t.TempDir())
	if err != nil || cleanup == nil {
		t.Fatalf("isolated kernel recovery: %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	kernel := strings.TrimSpace(string(data))
	if _, err := os.Stat(kernel); err != nil {
		t.Fatalf("private kernel was cleaned too early: %v", err)
	}
	cleanup()
	if _, err := os.Stat(kernel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary kernel not cleaned after unmount: %v", err)
	}
}

func TestGuestfsMountFailsClosedForOtherSuperminErrors(t *testing.T) {
	bin := t.TempDir()
	guestfsStub(t, bin, "guestmount", `echo 'libguestfs: supermin exited with error status 1: missing libraries' >&2; exit 27`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cleanup, err := MountReadOnly(context.Background(), "/tmp/guest.qcow2", t.TempDir())
	if err == nil || cleanup != nil || !strings.Contains(err.Error(), "missing libraries") {
		t.Fatalf("unrelated supermin failure accepted: cleanup=%v err=%v", cleanup != nil, err)
	}
}

func TestGuestfsMountCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if cleanup, err := MountReadOnly(ctx, "/tmp/guest.qcow2", t.TempDir()); cleanup != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled mount: cleanup=%v err=%v", cleanup != nil, err)
	}
}
