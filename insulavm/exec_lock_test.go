package insulavm

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVMExecLockRejectsConcurrentSameUUID(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "insula.db")
	const idA = "019a5d11-1234-7123-8123-123456789abc"
	const idB = "019a5d11-1234-7123-8123-123456789abe"

	unlockA, err := acquireVMExecLock(registryPath, idA)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockA()

	if unlockSecond, err := acquireVMExecLock(registryPath, idA); !errors.Is(err, ErrVMExecBusy) || unlockSecond != nil ||
		!strings.Contains(err.Error(), idA) || !strings.Contains(err.Error(), "retry") {
		t.Fatalf("same VM must be rejected with actionable error, got release=%v err=%v", unlockSecond != nil, err)
	}
	unlockB, err := acquireVMExecLock(registryPath, idB)
	if err != nil {
		t.Fatalf("independent VMs must be executable concurrently: %v", err)
	}
	unlockB()
	unlockA()

	unlockAgain, err := acquireVMExecLock(registryPath, idA)
	if err != nil {
		t.Fatalf("completed exec must release the lock: %v", err)
	}
	unlockAgain()
}

func TestVMExecLockRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "insula.db")
	const id = "019a5d11-1234-7123-8123-123456789abc"
	hash := sha256.Sum256([]byte(id))
	path := fmt.Sprintf("%s.vm-exec-%x.lock", db, hash)
	target := filepath.Join(root, "sensitive")
	if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	unlock, err := acquireVMExecLock(db, id)
	if err == nil || unlock != nil {
		t.Fatalf("symlink lock path must be rejected, release=%v err=%v", unlock != nil, err)
	}
	bytes, err := os.ReadFile(target)
	if err != nil || string(bytes) != "untouched" {
		t.Fatalf("unexpected write through lock symlink: %q %v", bytes, err)
	}
}

func TestVMExecLockAcrossProcesses(t *testing.T) {
	if os.Getenv("INSULA_EXEC_LOCK_CHILD") == "1" {
		unlock, err := acquireVMExecLock(os.Getenv("INSULA_EXEC_LOCK_DB"), os.Getenv("INSULA_EXEC_LOCK_ID"))
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		fmt.Println("LOCKED")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return
	}

	const id = "019a5d11-1234-7123-8123-123456789abc"
	db := filepath.Join(t.TempDir(), "insula.db")
	cmd := exec.Command(os.Args[0], "-test.run=^TestVMExecLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "INSULA_EXEC_LOCK_CHILD=1", "INSULA_EXEC_LOCK_DB="+db, "INSULA_EXEC_LOCK_ID="+id)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "LOCKED" {
		t.Fatalf("child did not obtain lock: line=%q err=%v", line, err)
	}
	if unlock, err := acquireVMExecLock(db, id); !errors.Is(err, ErrVMExecBusy) || unlock != nil {
		t.Fatalf("a second CLI/process must see the busy VM: release=%v err=%v", unlock != nil, err)
	}
	if _, err := stdin.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child lock holder exited abnormally: %v", err)
	}
	unlock, err := acquireVMExecLock(db, id)
	if err != nil {
		t.Fatalf("lock not released when child process exited: %v", err)
	}
	unlock()
}
