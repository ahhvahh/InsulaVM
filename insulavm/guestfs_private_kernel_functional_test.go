//go:build integration

package insulavm

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFunctionalGuestfsPrivateKernelMount boots a real libguestfs appliance with
// an APT-extracted private kernel and mounts a disposable ext4 disk read-only.
// It is restricted to the KVM-enabled Integral 3 promotion workflow.
func TestFunctionalGuestfsPrivateKernelMount(t *testing.T) {
	if os.Getenv("INSULA_GUESTFS_FUNCTIONAL") != "1" {
		t.Skip("real libguestfs and kernel download are restricted to Integral 3")
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Fatalf("KVM is required for this functional test: %v", err)
	}
	for _, name := range []string{"mkfs.ext4", "guestmount", "guestunmount"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("%s is required: %v", name, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	privateEnv, cleanKernel, err := prepareGuestfsKernel(ctx)
	if err != nil {
		t.Fatalf("prepare real private kernel: %v", err)
	}
	kernel := strings.TrimPrefix(privateEnv[0], "SUPERMIN_KERNEL=")
	stage, err := os.MkdirTemp("", "insula-guestfs-functional-")
	if err != nil {
		cleanKernel()
		t.Fatal(err)
	}
	mountpoint := filepath.Join(stage, "mount")
	mounted := false
	t.Cleanup(func() {
		if mounted {
			unmountCtx, unmountCancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer unmountCancel()
			out, unmountErr := exec.CommandContext(unmountCtx, "guestunmount", mountpoint).CombinedOutput()
			if unmountErr != nil {
				t.Errorf("unmount failed; retain private kernel and staging at %s: %v: %s", stage, unmountErr, out)
				return
			}
			mounted = false
		}
		cleanKernel()
		_ = os.RemoveAll(stage)
	})

	source := filepath.Join(stage, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(mountpoint, 0700); err != nil {
		t.Fatal(err)
	}
	const proof = "insula-guestfs-private-kernel-functional"
	if err := os.WriteFile(filepath.Join(source, "proof.txt"), []byte(proof), 0600); err != nil {
		t.Fatal(err)
	}

	disk := filepath.Join(stage, "disk.raw")
	f, err := os.OpenFile(disk, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(128 << 20); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(ctx, "mkfs.ext4", "-F", "-d", source, disk).CombinedOutput()
	if err != nil {
		t.Fatalf("create ext4 image: %v: %s", err, out)
	}

	cmd := exec.CommandContext(ctx, "guestmount", "--ro", "-a", disk, "-m", "/dev/sda", mountpoint)
	cmd.Env = append(os.Environ(), privateEnv...)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real guestmount with extracted kernel: %v: %s", err, out)
	}
	mounted = true

	data, err := os.ReadFile(filepath.Join(mountpoint, "proof.txt"))
	if err != nil || string(data) != proof {
		t.Fatalf("read-only guestmount verification: data=%q err=%v", data, err)
	}

	unmountCtx, unmountCancel := context.WithTimeout(context.Background(), 40*time.Second)
	out, err = exec.CommandContext(unmountCtx, "guestunmount", mountpoint).CombinedOutput()
	unmountCancel()
	if err != nil {
		t.Fatalf("real guestunmount failed; staging retained at %s: %v: %s", stage, err, out)
	}
	mounted = false
	cleanKernel()
	if _, err := os.Stat(kernel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private kernel not removed after guestunmount: %v", err)
	}
	t.Log("real libguestfs mounted ext4 read-only with private kernel; staging cleaned")
}
