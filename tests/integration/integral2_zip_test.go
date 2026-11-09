//go:build integration

package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ahhvahh/InsulaVM/insulavm"
)

// TestIntegral2ZipTransferAndCommand verifies one real Debian VM through the
// public Go API and CLI, not by directly copying files into guest storage.
// The runner supplies a trusted cloud-init-ready QCOW2 and isolated config.
func TestIntegral2ZipTransferAndCommand(t *testing.T) {
	configPath := os.Getenv("INSULA_IT2_CONFIG")
	imagePath := os.Getenv("INSULA_IT2_QCOW2")
	cliPath := os.Getenv("INSULA_IT2_CLI")
	if configPath == "" && imagePath == "" && cliPath == "" {
		t.Skip("real KVM test requires scripts/tests/integral-2-zip.sh")
	}
	if configPath == "" || imagePath == "" || cliPath == "" {
		t.Fatal("INSULA_IT2_CONFIG, INSULA_IT2_QCOW2 and INSULA_IT2_CLI must all be set")
	}
	for _, path := range []string{configPath, imagePath, cliPath} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("required host file invalid: %s: %v", path, err)
		}
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Fatalf("real KVM required, no skip accepted: %v", err)
	}
	engine, err := insulavm.New(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := engine.Configuration()
	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	token := integral2Token(t)
	profileName := "integral-2-" + token
	// Profile copy is independent. The original downloaded source image remains
	// read-only throughout the VM lifecycle.
	err = engine.RegisterProfile(insulavm.Profile{
		Name: profileName, OS: "debian", VCPUs: 1, MemoryMiB: 1024, DiskGB: 8,
	}, imagePath)
	if err != nil {
		t.Fatalf("register Debian cloud profile: %v", err)
	}
	var managedVMID string
	t.Cleanup(func() {
		// Preserve the backing image if Libvirt cannot confirm the managed VM
		// has been removed. The runtime cleanup executes before this callback.
		if managedVMID != "" {
			checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			live, checkErr := engine.GetVMStatus(checkCtx, managedVMID)
			if checkErr != nil || live.LibvirtState != "absent" {
				t.Errorf("retaining profile because VM status is uncertain or still active: %+v %v", live, checkErr)
				return
			}
		}
		if err := os.RemoveAll(filepath.Join(cfg.Storage.Profiles, profileName)); err != nil {
			t.Errorf("remove disposable test profile: %v", err)
		}
	})

	var startupEvents []insulavm.VMProgress
	instance, err := engine.StartVMWithOptions(ctx, insulavm.VMStartOptions{
		Profile: profileName, MemoryMiB: 1024, VCPUs: 1,
		SSHReadyTimeout: 4 * time.Minute,
		// Preserve a failed guest for troubleshooting when startup reaches
		// Libvirt. Host runner is ephemeral; no other VM may be touched.
		PreserveOnFailure: false,
	}, func(progress insulavm.VMProgress) {
		startupEvents = append(startupEvents, progress)
		t.Logf("[boot:%s] %s", progress.Stage, progress.Message)
	})
	if err != nil {
		t.Fatalf("real Debian cloud boot/Cloud-Init/DHCP/SSH failed: %v", err)
	}
	vmID := instance.ID
	managedVMID = vmID
	stopped := false
	t.Cleanup(func() {
		if stopped {
			return
		}
		cleanupCtx, done := context.WithTimeout(context.Background(), 50*time.Second)
		defer done()
		result, stopErr := engine.StopVMWithOptions(cleanupCtx, insulavm.VMStopOptions{
			VMID: vmID, GracePeriod: 10 * time.Second, ForceAfterTimeout: true,
		})
		t.Errorf("Test Integral 2 did not finish normally, emergency cleanup result=%+v error=%v", result, stopErr)
	})
	if vmID == "" || instance.IPAddress == "" || len(startupEvents) == 0 {
		t.Fatalf("VM not ready or progress missing: id=%s ip=%s events=%d", vmID, instance.IPAddress, len(startupEvents))
	}
	initial, err := engine.GetVMStatus(ctx, vmID)
	if err != nil || !initial.IsRunning() {
		t.Fatalf("managed VM not running: %+v %v", initial, err)
	}
	t.Logf("managed VM running: id=%s ip=%s 1vCPU/1024MiB", vmID, instance.IPAddress)

	// API scenario: source ZIP is preserved as a single file by the existing
	// ImportToVM implementation (its internal TAR.GZ is only the transport).
	hostDir := t.TempDir()
	apiZip := integral2MakeZip(t, filepath.Join(hostDir, "api-payload.zip"), "api")
	var progress []insulavm.ImportProgress
	var importOutput bytes.Buffer
	imported, err := engine.ImportToVM(ctx, insulavm.VMImportRequest{
		VMID: vmID, SourcePath: apiZip.Path,
		Output: &importOutput,
		OnProgress: func(p insulavm.ImportProgress) {
			progress = append(progress, p)
			t.Logf("[import:%s] %d/%d bytes", p.Stage, p.BytesSent, p.TotalBytes)
		},
	})
	if err != nil || imported == nil || !imported.Imported {
		t.Fatalf("API ZIP upload failed: %+v %v\n%s", imported, err, importOutput.String())
	}
	if imported.Files != 1 || imported.OriginalBytes != apiZip.Size ||
		imported.DestinationPath != cfg.Guest.Destination {
		t.Fatalf("API ZIP import metadata mismatch: %+v expected_bytes=%d workspace=%s",
			imported, apiZip.Size, cfg.Guest.Destination)
	}
	for _, stage := range []string{"uploading", "verifying", "extracting", "completed"} {
		if !integral2HasStage(progress, stage) {
			t.Fatalf("API import missing progress stage %q: %+v", stage, progress)
		}
	}
	apiRun := func(command, marker string) {
		t.Helper()
		var output bytes.Buffer
		result, commandErr := engine.ExecVM(ctx, insulavm.ExecRequest{
			VMID: vmID, Command: command, CommandTimeout: time.Minute, Output: &output,
		})
		t.Logf("[go:exec] %s", strings.TrimSpace(output.String()))
		if commandErr != nil || result == nil || result.LogDirectory == "" ||
			!strings.Contains(output.String(), marker) {
			t.Fatalf("Go ExecVM command/response mismatch: %v result=%+v marker=%q stdout=%q",
				commandErr, result, marker, output.String())
		}
		if _, statErr := os.Stat(result.LogDirectory); statErr != nil {
			t.Fatalf("Go ExecVM did not retain command logs: %v", statErr)
		}
	}
	for _, check := range integral2GuestCommands(apiZip) {
		apiRun(check.Command, check.Marker)
	}
	// A failed guest command must propagate as a Go error, rather than being
	// mistaken for a successful transport over SSH.
	var rejected bytes.Buffer
	_, err = engine.ExecVM(ctx, insulavm.ExecRequest{
		VMID: vmID, Command: "test 4 -eq 5", Output: &rejected,
		CommandTimeout: 20 * time.Second,
	})
	if err == nil {
		t.Fatal("ExecVM swallowed nonzero guest exit code")
	}

	// CLI parity scenario: different ZIP and same engine, but through the
	// user-facing insula vm import / insula vm exec commands.
	cliZip := integral2MakeZip(t, filepath.Join(hostDir, "cli-payload.zip"), "cli")
	runCLI := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, cliPath, args...)
		out, commandErr := cmd.CombinedOutput()
		t.Logf("[cli] %s: %s", strings.Join(args[:2], " "), strings.TrimSpace(string(out)))
		if commandErr != nil {
			t.Fatalf("CLI command failed %q: %v: %s", args, commandErr, out)
		}
		return string(out)
	}
	cliOutput := runCLI("vm", "import", "-config", configPath, "-id", vmID, "-source", cliZip.Path)
	if !strings.Contains(cliOutput, "Imported 1 file(s)") ||
		!strings.Contains(cliOutput, "import:completed") {
		t.Fatalf("CLI import did not confirm uploaded ZIP: %s", cliOutput)
	}
	for _, check := range integral2GuestCommands(cliZip) {
		cliOutput = runCLI("vm", "exec", "-config", configPath, "-id", vmID, "-command", check.Command)
		if !strings.Contains(cliOutput, check.Marker) {
			t.Fatalf("CLI remote response missing marker %q: %s", check.Marker, cliOutput)
		}
	}
	// Status must still report running after independent upload/exec calls.
	alive, err := engine.GetVMStatus(ctx, vmID)
	if err != nil || !alive.IsRunning() {
		t.Fatalf("guest did not remain running after upload/commands: %+v %v", alive, err)
	}
	stop, err := engine.StopVMWithOptions(ctx, insulavm.VMStopOptions{
		VMID: vmID, GracePeriod: 45 * time.Second, ForceAfterTimeout: false,
	})
	if err != nil || stop == nil || !stop.Graceful || stop.Forced || stop.TimedOut {
		t.Fatalf("VM failed graceful stop; no forced stop counts as success: %+v %v", stop, err)
	}
	after, err := engine.GetVMStatus(ctx, vmID)
	if err != nil || after.LibvirtState != "absent" {
		t.Fatalf("guest still exists in Libvirt: %+v %v", after, err)
	}
	if _, err := os.Stat(instance.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("instance overlay/SSH secrets were not removed: %s err=%v", instance.Directory, err)
	}
	stopped = true
	t.Log("INSULA_INTEGRAL_2_OK: VM boot + Go ZIP transfer + CLI ZIP transfer + guest counts/hashes + remote errors + clean stop")
}

type integral2Archive struct {
	Path string
	Base string
	Count int
	Size int64
	ZipSHA string
	BinSHA string
	Label string
}

type integral2Check struct {
	Command string
	Marker string
}

func integral2MakeZip(t *testing.T, path, label string) integral2Archive {
	t.Helper()
	file, err := os.Create(path)
	if err != nil { t.Fatal(err) }
	writer := zip.NewWriter(file)
	count := 12
	binData := []byte{0, 1, 2, 3, 0, 255, 254, 10, 32, 97, 98}
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("folder-%02d/file-%02d.txt", i%3, i)
		data := []byte(strings.Repeat(fmt.Sprintf("InsulaVM-%s-%02d\n", label, i), i+1))
		if i == 0 {
			name = "folder-00/binary.dat"
			data = binData
		}
		entry, createErr := writer.Create(name)
		if createErr != nil { t.Fatal(createErr) }
		if _, writeErr := entry.Write(data); writeErr != nil { t.Fatal(writeErr) }
	}
	if err := writer.Close(); err != nil { t.Fatal(err) }
	if err := file.Close(); err != nil { t.Fatal(err) }
	payload, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	if len(payload) == 0 { t.Fatal("ZIP unexpectedly empty") }
	zipSum := sha256.Sum256(payload)
	binSum := sha256.Sum256(binData)
	return integral2Archive{
		Path: path, Base: filepath.Base(path), Count: count,
		Size: int64(len(payload)), Label: label,
		ZipSHA: hex.EncodeToString(zipSum[:]), BinSHA: hex.EncodeToString(binSum[:]),
	}
}

func integral2GuestCommands(p integral2Archive) []integral2Check {
	name := p.Base // fixed, trusted test fixture name, not untrusted user input
	target := "extracted-" + p.Label
	return []integral2Check{
		{
			Command: "test -s " + name + " && printf 'INSULA_IT2_ZIP_SHA=%s\n' \"$(sha256sum " + name + " | cut -d' ' -f1)\"",
			Marker: "INSULA_IT2_ZIP_SHA=" + p.ZipSHA,
		},
		{
			Command: "python3 -c 'import zipfile; z=zipfile.ZipFile(\"" + name +
				"\"); print(\"INSULA_IT2_ZIP_ENTRIES=\"+str(sum(not x.is_dir() for x in z.infolist())))'",
			Marker: "INSULA_IT2_ZIP_ENTRIES=" + strconv.Itoa(p.Count),
		},
		{
			Command: "python3 -m zipfile -e " + name + " " + target + " && printf 'INSULA_IT2_UNPACK_OK\n'",
			Marker: "INSULA_IT2_UNPACK_OK",
		},
		{
			Command: "test \"$(find " + target + " -type f | wc -l)\" -eq " + strconv.Itoa(p.Count) +
				" && printf 'INSULA_IT2_EXTRACTED=%s\n' \"$(find " + target + " -type f | wc -l)\"",
			Marker: "INSULA_IT2_EXTRACTED=" + strconv.Itoa(p.Count),
		},
		{
			Command: "printf 'INSULA_IT2_BINARY_SHA=%s\n' \"$(sha256sum " + target +
				"/folder-00/binary.dat | cut -d' ' -f1)\"",
			Marker: "INSULA_IT2_BINARY_SHA=" + p.BinSHA,
		},
	}
}

func integral2HasStage(events []insulavm.ImportProgress, stage string) bool {
	for _, event := range events {
		if event.Stage == stage { return true }
	}
	return false
}

func integral2Token(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil { t.Fatal(err) }
	return hex.EncodeToString(raw)
}

func TestIntegral2ZipFixtureContract(t *testing.T) {
	tmp := t.TempDir()
	p := integral2MakeZip(t, filepath.Join(tmp, "api-payload.zip"), "api")
	if p.Count != 12 || p.Size == 0 || p.ZipSHA == "" || p.BinSHA == "" {
		t.Fatalf("invalid ZIP fixture: %+v", p)
	}
	reader, err := zip.OpenReader(p.Path)
	if err != nil { t.Fatal(err) }
	defer reader.Close()
	if len(reader.File) != p.Count { t.Fatalf("ZIP contains %d entries, expected %d", len(reader.File), p.Count) }
	for _, check := range integral2GuestCommands(p) {
		if check.Command == "" || check.Marker == "" {
			t.Fatalf("invalid remote check: %+v", check)
		}
	}
}
