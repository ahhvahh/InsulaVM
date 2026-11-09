//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ahhvahh/InsulaVM/insulavm"
)

const (
	integral3MemoryMiB = 500
	integral3VCPUs     = 1
	integral3DiskGiB   = 10
	integral3GuestDir  = "/tmp/workspace/created"
	integral3GuestFile = integral3GuestDir + "/state.txt"
)

// TestIntegral3ISOWorkspacePersistence exercises the ACTUAL installer ISO,
// InsulaVM's Go VM lifecycle, FreezeVM+Live ISO conversion, a second KVM boot
// from the generated ISO, remote SSH commands and the original /tmp data.
// No host-side copying, bind-mount or re-injection of the marker is permitted.
func TestIntegral3ISOWorkspacePersistence(t *testing.T) {
	configPath := os.Getenv("INSULA_IT3_CONFIG")
	installerISO := os.Getenv("INSULA_IT3_INSTALLER_ISO")
	cliPath := os.Getenv("INSULA_IT3_CLI")
	if configPath == "" && installerISO == "" && cliPath == "" {
		t.Skip("real ISO installer test requires scripts/tests/integral-3-iso-persistence.sh")
	}
	if configPath == "" || installerISO == "" || cliPath == "" {
		t.Fatal("required: INSULA_IT3_CONFIG, INSULA_IT3_INSTALLER_ISO, INSULA_IT3_CLI")
	}
	for _, path := range []string{configPath, installerISO, cliPath} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("file unavailable/empty: %s: %v", path, err)
		}
	}
	if stat, err := os.Stat("/dev/kvm"); err != nil || stat.Mode()&os.ModeDevice == 0 {
		t.Fatalf("KVM unavailable (cannot skip mandatory real boot): %v", err)
	}
	engine, err := insulavm.New(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := engine.Configuration()
	if cfg.Guest.Destination != "/tmp/workspace" {
		t.Fatalf("integral-3 requires guest.destination=/tmp/workspace, got %q", cfg.Guest.Destination)
	}
	if cfg.SSH.SudoMode != "passwordless" {
		t.Fatalf("freeze requires ssh.sudo_mode=passwordless, got %q", cfg.SSH.SudoMode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 125*time.Minute)
	defer cancel()
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	tag := hex.EncodeToString(raw)
	baseName := "it3-installer-" + tag
	frozenName := "it3-frozen-" + tag
	marker := "INSULA_IT3_PERSIST_" + tag
	expectedSHA := integral3SHA(marker + "\n")
	var sourceID, restoredID string
	var sourceDir, restoredDir string
	var liveISOPath string
	defer func() {
		// Preserve every collected application log, even after a failed test.
		t.Logf("integral-3 evidence: logs=%s installer_profile=%s frozen_profile=%s iso=%s",
			cfg.Storage.Logs, baseName, frozenName, liveISOPath)
	}()
	t.Cleanup(func() {
		// Only our private, randomly named VM IDs can be considered for cleanup.
		for _, id := range []string{restoredID, sourceID} {
			if id == "" {
				continue
			}
			checkCtx, done := context.WithTimeout(context.Background(), 60*time.Second)
			state, stateErr := engine.GetVMStatus(checkCtx, id)
			if stateErr == nil && state.IsRunning() {
				_, stopErr := engine.StopVMWithOptions(checkCtx, insulavm.VMStopOptions{
					VMID: id, GracePeriod: 12 * time.Second, ForceAfterTimeout: true,
				})
				if stopErr != nil {
					t.Errorf("emergency stop of owned VM %s: %v", id, stopErr)
				}
			} else if stateErr != nil {
				t.Errorf("cannot determine cleanup safety for %s: %v", id, stateErr)
			}
			done()
		}
	})
	t.Logf("installer ISO=%s; installed disk=10 GiB; running VMs=1 vCPU/500 MiB", installerISO)

	// CreateProfileFromISO intentionally requires 1024 MiB to RUN THE
	// INSTALLER. The first managed VM and the exported ISO VM are both 500 MiB.
	// This exception is a published API precondition, not a relaxation of the
	// runtime resource contract.
	installed, err := engine.CreateProfileFromISO(ctx, insulavm.CreateProfileFromISORequest{
		Name: baseName, ISOPath: installerISO,
		MemoryMiB: 1024, VCPUs: integral3VCPUs, DiskGB: integral3DiskGiB,
		Timeout: 90 * time.Minute,
		OnProgress: func(p insulavm.ProfileISOProgress) {
			t.Logf("[install:%s] %s", p.Stage, p.Message)
		},
	})
	if err != nil || installed == nil || !installed.Installed ||
		installed.Profile == nil || installed.ImagePath == "" {
		t.Fatalf("installer ISO -> independent QCOW2 profile failed: result=%+v err=%v", installed, err)
	}
	integral3AssertDiskSize(t, installed.ImagePath, integral3DiskGiB)
	t.Logf("[install:ready] verified 10 GiB QCOW2 %s", installed.ImagePath)

	var runtimeEvents []insulavm.VMProgress
	source, err := engine.StartVMWithOptions(ctx, insulavm.VMStartOptions{
		Profile: baseName, MemoryMiB: integral3MemoryMiB, VCPUs: integral3VCPUs,
	}, func(p insulavm.VMProgress) {
		runtimeEvents = append(runtimeEvents, p)
		t.Logf("[source:%s] %s", p.Stage, p.Message)
	})
	if err != nil {
		t.Fatalf("boot 500 MiB source VM failed: %v", err)
	}
	sourceID, sourceDir = source.ID, source.Directory
	if source.ID == "" || source.IPAddress == "" || len(runtimeEvents) == 0 {
		t.Fatalf("source not SSH-ready: %+v", source)
	}
	integral3AssertDomainResources(t, cfg.Virtualization.LibvirtURI, source.ID, integral3MemoryMiB, integral3VCPUs)

	// New unique file is created ONLY by sending a command to the guest.
	// Each ExecVM session closes when the command returns (no interactive
	// shell remains active before freezing).
	createCommand := "set -eu; " +
		"mkdir -p " + integral3GuestDir + "; " +
		"printf '%s\\n' '" + marker + "' > " + integral3GuestFile + "; " +
		"test -d " + integral3GuestDir + "; test -s " + integral3GuestFile + "; " +
		"sync; printf 'INSULA_IT3_CREATED\\n'; exit 0"
	integral3ExecGuest(t, ctx, engine, source.ID, createCommand, "INSULA_IT3_CREATED")

	// Verify correct bytes at source through a SECOND, independent SSH
	// connection before any freeze/export takes place.
	hashCommand := "set -eu; test -d " + integral3GuestDir +
		"; test -f " + integral3GuestFile +
		"; printf 'INSULA_IT3_SHA=%s\\n' \"$(sha256sum " +
		integral3GuestFile + " | cut -d' ' -f1)\""
	integral3ExecGuest(t, ctx, engine, source.ID, hashCommand, "INSULA_IT3_SHA="+expectedSHA)
	integral3ExecGuest(t, ctx, engine, source.ID,
		"findmnt -n -o FSTYPE -T "+integral3GuestFile+" && echo INSULA_IT3_FS_PROBED",
		"INSULA_IT3_FS_PROBED")

	// InsulaVM's canonical Go pipeline prepares live-boot, removes
	// per-instance identity, requests graceful shutdown, freezes QCOW2 and
	// constructs the standalone ISO. No forced shutdown is acceptable.
	exported, err := engine.ExportVMToLiveISO(ctx, insulavm.VMExportISORequest{
		VMID: source.ID, Name: frozenName,
		PrepareLiveBoot: true,
		ShutdownTimeout: 4 * time.Minute, CommandTimeout: 20 * time.Minute,
		OnProgress: func(p insulavm.VMFreezeProgress) {
			t.Logf("[freeze:%s] %s", p.Stage, p.Message)
		},
	})
	if err != nil || exported == nil || exported.Freeze == nil ||
		!exported.Freeze.SourceStopped || !exported.Freeze.SourceCleaned ||
		exported.ISOPath == "" {
		t.Fatalf("freeze+ISO failed or was forced; source evidence kept: result=%+v err=%v", exported, err)
	}
	liveISOPath = exported.ISOPath
	stat, err := os.Stat(liveISOPath)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() == 0 {
		t.Fatalf("generated Live ISO missing/empty: %s: %v", liveISOPath, err)
	}
	integral3AssertDiskSize(t, exported.Freeze.ImagePath, integral3DiskGiB)
	oldState, err := engine.GetVMStatus(ctx, source.ID)
	if err != nil || oldState.LibvirtState != "absent" {
		t.Fatalf("source domain still active after freeze: %+v err=%v", oldState, err)
	}
	if _, err := os.Stat(sourceDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source transient overlay/keys not deleted: %s err=%v", sourceDir, err)
	}

	// Boot a DIFFERENT VM from the ACTUAL ISO, not from frozen QCOW2. The
	// scratch disk is disposable and is never populated with the marker.
	restored, err := engine.StartVMFromISO(ctx, insulavm.LiveISOStartOptions{
		Profile: frozenName, ISOPath: liveISOPath,
		MemoryMiB: integral3MemoryMiB, VCPUs: integral3VCPUs,
	})
	if err != nil {
		t.Fatalf("second VM boot from generated ISO failed (check serial/Libvirt logs): %v", err)
	}
	restoredID, restoredDir = restored.ID, restored.Directory
	if restored.ID == source.ID || restored.IPAddress == "" {
		t.Fatalf("second VM must have a different identity and SSH address: source=%s guest=%+v", source.ID, restored)
	}
	integral3AssertDomainResources(t, cfg.Virtualization.LibvirtURI, restored.ID, integral3MemoryMiB, integral3VCPUs)

	// Files must exist in the NEW VM before any write/import is performed.
	// Marker check is content-addressed to avoid a stale/incorrect directory
	// or a false positive from another run.
	integral3ExecGuest(t, ctx, engine, restored.ID, hashCommand, "INSULA_IT3_SHA="+expectedSHA)
	integral3ExecGuest(t, ctx, engine, restored.ID,
		"set -eu; test -d "+integral3GuestDir+
			"; test \"$(cat "+integral3GuestFile+")\" = '"+marker+
			"'; printf 'INSULA_IT3_RESTORED_OK\\n'; exit 0",
		"INSULA_IT3_RESTORED_OK")
	t.Logf("[ISO] /tmp/workspace persisted with exact SHA-256=%s", expectedSHA)

	// Cross-check the same result using the CLI frontend, which shares the
	// Engine. This also verifies the guest shell exits cleanly between calls.
	cli := exec.CommandContext(ctx, cliPath, "vm", "exec",
		"-config", configPath, "-id", restored.ID,
		"-command", "test -d "+integral3GuestDir+
			" && test -s "+integral3GuestFile+
			" && echo INSULA_IT3_CLI_READ_OK")
	cliOutput, cliErr := cli.CombinedOutput()
	if cliErr != nil || !bytes.Contains(cliOutput, []byte("INSULA_IT3_CLI_READ_OK")) {
		t.Fatalf("CLI / ISO guest parity failed: err=%v output=%q", cliErr, cliOutput)
	}

	stopped, err := engine.StopVMWithOptions(ctx, insulavm.VMStopOptions{
		VMID: restored.ID, GracePeriod: 50 * time.Second, ForceAfterTimeout: false,
	})
	if err != nil || stopped == nil || !stopped.Graceful || stopped.Forced || stopped.TimedOut {
		t.Fatalf("second VM not shut down gracefully: result=%+v err=%v", stopped, err)
	}
	stoppedState, err := engine.GetVMStatus(ctx, restored.ID)
	if err != nil || stoppedState.LibvirtState != "absent" {
		t.Fatalf("second VM still exists in Libvirt: %+v err=%v", stoppedState, err)
	}
	if _, err := os.Stat(restoredDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second VM scratch/storage/keys not deleted: %s err=%v", restoredDir, err)
	}
	t.Log("INSULA_INTEGRAL_3_OK: mini ISO -> 10GiB installed disk -> 500MiB/1CPU managed guest -> saved /tmp/workspace -> graceful freeze -> Live ISO -> fresh 500MiB/1CPU guest -> same file SHA -> graceful stop")
}

func integral3SHA(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func integral3ExecGuest(t *testing.T, ctx context.Context, engine *insulavm.Engine, vmID, command, marker string) {
	t.Helper()
	var output bytes.Buffer
	result, err := engine.ExecVM(ctx, insulavm.ExecRequest{
		VMID: vmID, Command: command,
		CommandTimeout: 3 * time.Minute, Output: &output,
	})
	t.Logf("[guest:%s] %s", vmID, strings.TrimSpace(output.String()))
	if err != nil || result == nil || result.LogDirectory == "" ||
		!strings.Contains(output.String(), marker) {
		t.Fatalf("guest command response mismatch (want %q): result=%+v err=%v output=%q",
			marker, result, err, output.String())
	}
	if _, err := os.Stat(result.LogDirectory); err != nil {
		t.Fatalf("guest execution log missing: %v", err)
	}
}

func integral3AssertDiskSize(t *testing.T, imagePath string, gib int) {
	t.Helper()
	out, err := exec.Command("qemu-img", "info", "--output=json", imagePath).Output()
	if err != nil {
		t.Fatalf("qemu-img info %s: %v", imagePath, err)
	}
	var info struct {
		VirtualSize int64  `json:"virtual-size"`
		Format      string `json:"format"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatalf("invalid qemu-img info json: %v", err)
	}
	if info.Format != "qcow2" || info.VirtualSize != int64(gib)*1024*1024*1024 {
		t.Fatalf("expected %d GiB qcow2, got size=%d format=%s", gib, info.VirtualSize, info.Format)
	}
}

func integral3AssertDomainResources(t *testing.T, uri, id string, memoryMiB, vcpus int) {
	t.Helper()
	out, err := exec.Command("virsh", "-c", uri, "dumpxml", id).Output()
	if err != nil {
		t.Fatalf("inspect actual Libvirt domain resources: %v", err)
	}
	var domain struct {
		Memory struct {
			Value int    `xml:",chardata"`
			Unit  string `xml:"unit,attr"`
		} `xml:"memory"`
		VCPU int `xml:"vcpu"`
	}
	if err := xml.Unmarshal(out, &domain); err != nil {
		t.Fatalf("invalid Libvirt domain XML: %v", err)
	}
	actual := domain.Memory.Value
	if domain.Memory.Unit == "KiB" {
		actual /= 1024
	}
	if actual != memoryMiB || domain.VCPU != vcpus {
		t.Fatalf("expected %d MiB/%d vCPU; got %d %s/%d vCPU", memoryMiB, vcpus,
			domain.Memory.Value, domain.Memory.Unit, domain.VCPU)
	}
}

func TestIntegral3MarkerContract(t *testing.T) {
	if integral3SHA("INSULA_IT3_PERSIST_test\n") !=
		"ce07ea9ba3eed09c442370cacbe4aa1139fffd238252dded6a2cc189e5f651e4" {
		t.Fatal("SHA-256 fixture changed unexpectedly")
	}
	if integral3MemoryMiB != 500 || integral3VCPUs != 1 || integral3DiskGiB != 10 ||
		!strings.HasPrefix(integral3GuestFile, "/tmp/workspace/") {
		t.Fatal("mandatory resource/path contract changed")
	}
}
