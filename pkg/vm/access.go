package vm

import (
 "context"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "syscall"
)

// grantQEMUStorageAccess grants an explicitly configured QEMU UID only the
// permissions needed to access the VM image files. Directory ACLs confer
// traversal (--x) without listing (r) or writing (w). The client SSH key
// and Cloud-Init user-data remain private to the launching user.
//
// The host operator must explicitly opt in with virtualization.qemu_uid,
// which must match the UID from libvirt's "as uid:NNN" error. The program
// never guesses the account or relaxes directories for all users.
func grantQEMUStorageAccess(ctx context.Context, uid int, baseImage, overlay, seedISO string) error {
 if uid == 0 { return nil }
 if uid < 0 || uid > 2147483647 {
  return errors.New("invalid qemu uid: only a non-root Linux UID is permitted")
 }
 if ctx == nil { return errors.New("context is required") }
 if _, err := exec.LookPath("setfacl"); err != nil {
  return fmt.Errorf("QEMU ACL provisioning requires setfacl (Debian: sudo apt install acl): %w", err)
 }
 // Prepare the full ancestry of all three files, including $HOME when the
 // operator deliberately stores a VM profile under a private home directory.
 // Root-owned ancestors with search permission for others need no changes.
 checked := map[string]bool{}
 for _, file := range []string{baseImage, overlay, seedISO} {
  resolved, err := filepath.EvalSymlinks(file)
  if err != nil { return fmt.Errorf("resolve VM storage file %q: %w", file, err) }
  dir := filepath.Dir(resolved)
  for dir != "" && dir != string(filepath.Separator) {
   if !checked[dir] {
    checked[dir] = true
    info, err := os.Stat(dir)
    if err != nil { return fmt.Errorf("inspect VM storage directory %q: %w", dir, err) }
    if !info.IsDir() { return fmt.Errorf("VM storage parent is not a directory: %s", dir) }
    // The QEMU identity may already own a directory. Also, world-searchable
    // parent directories do not need named-user ACL entries.
    if !ownedBy(info, uid) && info.Mode().Perm()&0001 == 0 {
     if !ownedBy(info, os.Geteuid()) {
      return fmt.Errorf("QEMU uid %d cannot traverse %q: run sudo setfacl -m u:%d:--x %q once on the host", uid, dir, uid, dir)
     }
     if err := setACL(ctx, uid, "--x", dir); err != nil { return err }
    }
   }
   parent := filepath.Dir(dir)
   if parent == dir { break }
   dir = parent
  }
 }
 for _, spec := range []struct{path,perm string}{
  {baseImage, "r--"}, {overlay, "rw-"}, {seedISO, "r--"},
 } {
  resolved, err := filepath.EvalSymlinks(spec.path)
  if err != nil { return fmt.Errorf("resolve VM disk %q: %w", spec.path, err) }
  info, err := os.Stat(resolved)
  if err != nil { return err }
  if !info.Mode().IsRegular() { return fmt.Errorf("VM storage file is not regular: %s", resolved) }
  if ownedBy(info, uid) && fileHasOwnerPermission(info.Mode().Perm(), spec.perm) { continue }
  if !ownedBy(info, os.Geteuid()) {
   return fmt.Errorf("cannot grant QEMU uid %d access to %q, not owned by current user; configure file ACL as administrator", uid, resolved)
  }
  if err := setACL(ctx, uid, spec.perm, resolved); err != nil { return err }
 }
 return nil
}

func fileHasOwnerPermission(mode os.FileMode, perm string) bool {
 if strings.Contains(perm, "r") && mode&0400 == 0 { return false }
 if strings.Contains(perm, "w") && mode&0200 == 0 { return false }
 return true
}

func ownedBy(info os.FileInfo, uid int) bool {
 st, ok := info.Sys().(*syscall.Stat_t)
 return ok && st.Uid == uint32(uid)
}

func setACL(ctx context.Context, uid int, perm, path string) error {
 spec := "u:" + strconv.Itoa(uid) + ":" + perm
 out, err := exec.CommandContext(ctx, "setfacl", "-m", spec, path).CombinedOutput()
 if err != nil {
  return fmt.Errorf("grant QEMU uid %d %s access to %q: %w: %s", uid, perm, path, err, strings.TrimSpace(string(out)))
 }
 return nil
}
