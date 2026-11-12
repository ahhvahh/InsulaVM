package guestfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MountReadOnly uses libguestfs without exposing or modifying the
// backing image. Some minimal Linux hosts (including CI runners) have KVM but
// no readable kernel for supermin's *separate* libguestfs appliance. In that
// specific case, provision a distro kernel in a private temporary directory
// without installing packages, changing the host, or modifying the tests.
// The returned cleanup must run AFTER guestunmount, never before.
func MountReadOnly(ctx context.Context, image, mountpoint string) (func(), error) {
	mount := func(extraEnv ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "guestmount", "--ro", "-a", image, "-i", mountpoint)
		if len(extraEnv) != 0 {
			cmd.Env = append(os.Environ(), extraEnv...)
		}
		return cmd.CombinedOutput()
	}
	output, err := mount()
	if err == nil {
		return func() {}, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	original := fmt.Errorf("guestmount read-only failed: %w: %s", err, strings.TrimSpace(string(output)))
	if !strings.Contains(strings.ToLower(string(output)), "supermin") {
		return nil, original
	}

	// A generic exit status from supermin is not a diagnosis. Request the
	// documented detailed output before considering a targeted recovery.
	debugOutput, debugErr := mount("LIBGUESTFS_DEBUG=1", "LIBGUESTFS_TRACE=1")
	if debugErr == nil {
		return func() {}, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	diagnostic := tailGuestfsOutput(string(debugOutput))
	if !guestfsNeedsReadableKernel(string(debugOutput)) {
		return nil, fmt.Errorf("%w; libguestfs diagnostics: %s", original, diagnostic)
	}
	if os.Getenv("SUPERMIN_KERNEL") != "" || os.Getenv("SUPERMIN_MODULES") != "" {
		return nil, fmt.Errorf("%w; supermin kernel was explicitly configured; diagnostics: %s", original, diagnostic)
	}

	kernelEnv, cleanup, prepareErr := prepareGuestfsKernel(ctx)
	if prepareErr != nil {
		return nil, fmt.Errorf("%w; supermin cannot use a readable host kernel; private kernel preparation failed: %v; diagnostics: %s", original, prepareErr, diagnostic)
	}
	// The private-kernel attempt must also capture libguestfs/supermin trace.
	// A second generic exit code cannot diagnose a failure in real KVM CI.
	retryOutput, retryErr := mount(append(kernelEnv, "LIBGUESTFS_DEBUG=1", "LIBGUESTFS_TRACE=1")...)
	if retryErr != nil {
		cleanup()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w; guestmount with private supermin kernel failed: %v; libguestfs diagnostics: %s", original, retryErr, tailGuestfsOutput(string(retryOutput)))
	}
	return cleanup, nil
}

// guestfsNeedsReadableKernel only permits a private kernel fallback when
// supermin reports either no suitable kernel or an unreadable host vmlinuz.
func guestfsNeedsReadableKernel(debugOutput string) bool {
 if strings.Contains(debugOutput,"failed to find a suitable kernel"){
  return true
 }
 // A generic Permission denied must never trigger the kernel fallback:
 // it may be a guest disk, libvirt, or unrelated host access failure.
 for _,line:=range strings.Split(debugOutput,"\n"){
  if strings.Contains(line,"cp: cannot open")&&
   strings.Contains(line,"/vmlinuz-")&&
   strings.Contains(line,"for reading: Permission denied"){
   return true
  }
 }
 return false
}

func tailGuestfsOutput(output string) string {
	const max = 8000
	output = strings.TrimSpace(output)
	if len(output) > max {
		return "..." + output[len(output)-max:]
	}
	return output
}

var guestfsPackageName = regexp.MustCompile("^linux-(?:image|modules)-[a-zA-Z0-9][a-zA-Z0-9.+~-]*$")

// prepareGuestfsKernel downloads only from the host's configured APT package
// indices, verifies the package names, and extracts files into an ephemeral
// directory. No sudo, apt install, dpkg install, or host filesystem mutation.
func prepareGuestfsKernel(ctx context.Context) ([]string, func(), error) {
	for _, tool := range []string{"apt-cache", "apt-get", "dpkg-deb"} {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, nil, fmt.Errorf("%s is required for kernel-less libguestfs hosts: %w", tool, err)
		}
	}
	var kernelPackage string
	for _, meta := range []string{"linux-image-generic", "linux-image-amd64"} {
		output, err := exec.CommandContext(ctx, "apt-cache", "depends", meta).CombinedOutput()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "Depends: linux-image-") {
				continue
			}
			candidate := strings.TrimSpace(strings.TrimPrefix(line, "Depends: "))
			if guestfsPackageName.MatchString(candidate) && len(candidate) > len("linux-image-") && candidate[len("linux-image-")] >= '0' && candidate[len("linux-image-")] <= '9' {
				kernelPackage = candidate
				break
			}
		}
		if kernelPackage != "" {
			break
		}
	}
	if kernelPackage == "" {
		return nil, nil, errors.New("APT has no suitable linux-image-generic or linux-image-amd64 kernel dependency")
	}
	packages := []string{kernelPackage}
	modulesPackage := strings.Replace(kernelPackage, "linux-image-", "linux-modules-", 1)
	if output, err := exec.CommandContext(ctx, "apt-cache", "show", modulesPackage).CombinedOutput(); err == nil && len(strings.TrimSpace(string(output))) > 0 {
		packages = append(packages, modulesPackage)
	}

	root, err := os.MkdirTemp("", "insula-guestfs-kernel-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	downloads := filepath.Join(root, "packages")
	extracted := filepath.Join(root, "root")
	cache := filepath.Join(root, "cache")
	if err := os.Mkdir(cache, 0700); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := os.Mkdir(downloads, 0700); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := os.Mkdir(extracted, 0700); err != nil {
		cleanup()
		return nil, nil, err
	}
	for _, pkg := range packages {
		if !guestfsPackageName.MatchString(pkg) {
			cleanup()
			return nil, nil, fmt.Errorf("invalid APT kernel package name %q", pkg)
		}
		cmd := exec.CommandContext(ctx, "apt-get", "download", pkg)
		cmd.Dir = downloads
		output, err := cmd.CombinedOutput()
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("apt-get download %s: %w: %s", pkg, err, tailGuestfsOutput(string(output)))
		}
	}
	archives, err := filepath.Glob(filepath.Join(downloads, "*.deb"))
	if err != nil || len(archives) < len(packages) {
		cleanup()
		return nil, nil, fmt.Errorf("APT did not download expected kernel packages: %v", err)
	}
	for _, archive := range archives {
		output, err := exec.CommandContext(ctx, "dpkg-deb", "-x", archive, extracted).CombinedOutput()
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("extract kernel package: %w: %s", err, tailGuestfsOutput(string(output)))
		}
	}
	_ = os.RemoveAll(downloads)
	kernels, err := filepath.Glob(filepath.Join(extracted, "boot", "vmlinuz-*"))
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	for _, kernel := range kernels {
		if fi, err := os.Stat(kernel); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		version := strings.TrimPrefix(filepath.Base(kernel), "vmlinuz-")
		for _, dir := range []string{"lib", "usr/lib"} {
			modules := filepath.Join(extracted, dir, "modules", version)
			if fi, err := os.Stat(modules); err == nil && fi.IsDir() {
				// Extraction does not run kernel post-install hooks. Both
				// indexes are needed by the real libguestfs appliance.
				indexes := []string{"modules.dep", "modules.dep.bin"}
				needsDepmod := false
				for _, name := range indexes {
					fi, statErr := os.Stat(filepath.Join(modules, name))
					if statErr != nil || !fi.Mode().IsRegular() {
						needsDepmod = true
						break
					}
				}
				if needsDepmod {
					depmod, lookupErr := exec.LookPath("depmod")
					if lookupErr != nil {
						cleanup()
						return nil, nil, fmt.Errorf("depmod (kmod package) is required to index extracted kernel modules: %w", lookupErr)
					}
					args := []string{"-b", extracted}
					if dir == "usr/lib" {
						// The extracted tree may lack the host's /lib symlink.
						args = append(args, "-m", "/usr/lib/modules")
					}
					args = append(args, version)
					output, depmodErr := exec.CommandContext(ctx, depmod, args...).CombinedOutput()
					if depmodErr != nil {
						cleanup()
						return nil, nil, fmt.Errorf("depmod private kernel %s failed: %w: %s", version, depmodErr, tailGuestfsOutput(string(output)))
					}
				}
				for _, name := range indexes {
					fi, statErr := os.Stat(filepath.Join(modules, name))
					if statErr != nil || !fi.Mode().IsRegular() {
						cleanup()
						return nil, nil, fmt.Errorf("depmod did not generate %s for private kernel %s", name, version)
					}
				}
				return []string{"SUPERMIN_KERNEL=" + kernel, "SUPERMIN_MODULES=" + modules, "LIBGUESTFS_CACHEDIR=" + cache}, cleanup, nil
			}
		}
	}
	cleanup()
	return nil, nil, errors.New("downloaded APT kernel has no matching vmlinuz and /lib/modules; refusing to mount guest disk")
}


// ErrUnmount marks an unresolved guestmount/FUSE mount. Callers must
// preserve staging rather than removing a potentially mounted filesystem.
var ErrUnmount = errors.New("guest filesystem remains mounted")

// UnmountReadOnly releases a read-only FUSE mount before removing the
// private supermin kernel. On unmount failure cleanup MUST NOT be called.
func UnmountReadOnly(mountpoint string, cleanup func()) error {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "guestunmount", mountpoint).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: guestunmount %s failed: %v: %s",
			ErrUnmount, mountpoint, err, tailGuestfsOutput(string(output)))
	}
	cleanup()
	return nil
}
