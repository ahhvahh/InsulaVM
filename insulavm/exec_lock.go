package insulavm

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"syscall"
)

// ErrVMExecBusy reports that another ExecVM call is active for this VM.
// Callers may use errors.Is; the CLI receives the same descriptive error.
var ErrVMExecBusy = errors.New("another vm exec is already running")

// acquireVMExecLock prevents overlapping ExecVM calls on the same VM across
// independent InsulaVM CLI and Go processes using the same SQLite registry.
// This is an immediate, advisory Linux lock: it does not create a queue.
// Lockfiles remain on disk to avoid races caused by unlinking a locked inode.
func acquireVMExecLock(registryPath, vmID string) (func(), error) {
	if registryPath == "" || vmID == "" {
		return nil, errors.New("registry path and VM ID are required for exec locking")
	}
	// Hash the untrusted ID so it cannot escape the registry directory or
	// choose an arbitrary lock filename.
	digest := sha256.Sum256([]byte(vmID))
	path := fmt.Sprintf("%s.vm-exec-%x.lock", registryPath, digest)
	fd, err := syscall.Open(path,
		syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("open VM exec lock: %w", err)
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("inspect VM exec lock: %w", err)
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		_ = syscall.Close(fd)
		return nil, errors.New("VM exec lock path is not a regular file")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = syscall.Close(fd)
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("VM %s: %w; retry after the current command finishes", vmID, ErrVMExecBusy)
		}
		return nil, fmt.Errorf("lock VM %s for exec: %w", vmID, err)
	}
	// Closing the descriptor releases flock even on process termination.
	return func() { _ = syscall.Close(fd) }, nil
}
