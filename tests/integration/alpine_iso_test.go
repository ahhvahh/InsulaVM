//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ahhvahh/InsulaVM/pkg/config"
)

// TestAlpineISOConsoleLifecycle proves that an unmodified Alpine Virtual ISO
// boots a real KVM guest, executes a shell command, and powers off. Unlike
// the Cloud-Init/SSH-based Debian tests, Alpine's live ISO is reached over its
// built-in ttyS0 serial login (root has no password on the live medium).
// Run through scripts/tests/alpine-iso.sh; ordinary unit suites need no KVM.
func TestAlpineISOConsoleLifecycle(t *testing.T) {
	isoPath := os.Getenv("INSULA_TEST_ALPINE_ISO")
	if isoPath == "" {
		t.Skip("real Alpine ISO supplied by scripts/tests/alpine-iso.sh")
	}
	configPath := os.Getenv("INSULA_TEST_CONFIG")
	if configPath == "" {
		t.Fatal("INSULA_TEST_CONFIG is required")
	}
	c, err := config.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	isoPath, err = filepath.Abs(isoPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(isoPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatalf("Alpine ISO unavailable or empty: %s: %v", isoPath, err)
	}
	if _, err := exec.LookPath("virsh"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Fatalf("KVM required for mandatory ISO integration: %v", err)
	}

	expectedVersion := os.Getenv("INSULA_TEST_ALPINE_VERSION")
	if expectedVersion == "" {
		expectedVersion = "3.24.4"
	}
	if err := os.MkdirAll(c.Storage.Logs, 0700); err != nil {
		t.Fatal(err)
	}
	logDir, err := os.MkdirTemp(c.Storage.Logs, "alpine-iso-")
	if err != nil {
		t.Fatal(err)
	}
	serialPath := filepath.Join(logDir, "serial.log")
	serialFile, err := os.OpenFile(serialPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer serialFile.Close()
	t.Logf("console evidence: %s", serialPath)

	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(idBytes)
	name := "insula-alpine-e2e-" + token
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	uri := c.Virtualization.LibvirtURI

	// A loopback-only TCP console avoids SSH, DHCP, guest agents and unsafe
	// writable serial socket paths on qemu:///system hosts.
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	_ = reservation.Close()
	xmlSpec := alpineISODomainXML(name, isoPath, port)
	xmlPath := filepath.Join(logDir, "domain.xml")
	if err := os.WriteFile(xmlPath, []byte(xmlSpec), 0600); err != nil {
		t.Fatal(err)
	}

	exists, err := alpineDomainExists(ctx, uri, name)
	if err != nil || exists {
		t.Fatalf("cannot safely allocate temporary Libvirt domain: exists=%v err=%v", exists, err)
	}
	// Always try to remove only this randomly named transient test domain.
	// A forced stop in cleanup is NOT considered successful guest shutdown.
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		present, queryErr := alpineDomainExists(cleanupCtx, uri, name)
		if queryErr != nil {
			t.Errorf("cannot verify Alpine domain cleanup: %v", queryErr)
			return
		}
		if present {
			out, destroyErr := alpineVirsh(cleanupCtx, uri, "destroy", name)
			t.Errorf("Alpine domain did not shut down normally; emergency destroy result: %v %s", destroyErr, out)
		}
	})

	if out, err := alpineVirsh(ctx, uri, "create", xmlPath); err != nil {
		t.Fatalf("Libvirt could not boot Alpine ISO: %v: %s", err, out)
	}
	t.Logf("Alpine domain started: %s (1 vCPU, 500 MiB)", name)
	rawDomain, err := alpineVirsh(ctx, uri, "dumpxml", name)
	if err != nil {
		t.Fatalf("cannot inspect running domain: %v: %s", err, rawDomain)
	}
	if err := alpineValidateResources(rawDomain); err != nil {
		t.Fatal(err)
	}

	console, err := alpineDialConsole(ctx, port)
	if err != nil {
		t.Fatalf("serial console inaccessible: %v", err)
	}
	defer console.Close()
	chunks := make(chan string, 64)
	go func() {
		defer close(chunks)
		buf := make([]byte, 4096)
		for {
			n, readErr := console.Read(buf)
			if n > 0 {
				select {
				case chunks <- string(buf[:n]):
				case <-ctx.Done():
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	// A CR elicits the current login prompt if the console connected late.
	if _, err := console.Write([]byte("\r")); err != nil {
		t.Fatal(err)
	}
	prompt := regexp.MustCompile("(?m)(?:^|\n)[^\n]{0,100}# *$")
	state := "login"
	transcript := ""
	guestLine := "INSULA_ALPINE_GUEST_OK_" + token + ":" + expectedVersion
	exitLine := "INSULA_ALPINE_EXIT_" + token + ":0"
	bootTimer := time.NewTimer(3 * time.Minute)
	defer bootTimer.Stop()
	for state != "done" {
		select {
		case <-ctx.Done():
			t.Fatalf("Alpine console interrupted in stage %s: %v; serial=%s", state, ctx.Err(), serialPath)
		case <-bootTimer.C:
			t.Fatalf("Alpine guest did not finish stage %s (boot/login/command): serial=%s", state, serialPath)
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatalf("Alpine console disconnected in stage %s; serial=%s", state, serialPath)
			}
			if _, err := serialFile.WriteString(chunk); err != nil {
				t.Fatal(err)
			}
			transcript += chunk
			if len(transcript) > 2<<20 {
				transcript = transcript[len(transcript)-(1<<20):]
			}
			switch state {
			case "login":
				if strings.Contains(strings.ToLower(transcript), "login:") {
					if _, err := console.Write([]byte("root\r")); err != nil {
						t.Fatal(err)
					}
					state = "shell"
				}
			case "shell":
				if strings.Contains(transcript, "Password:") {
					t.Fatal("unexpected Alpine live root password challenge")
				}
				if prompt.MatchString(alpineVisibleConsole(transcript)) {
					// Alpine ash sends ESC[6n to query the terminal cursor.
					// Respond so that shell input is accepted immediately.
					if strings.Contains(transcript, "\x1b[6n") {
						if _, err := console.Write([]byte("\x1b[1;1R")); err != nil {
							t.Fatal(err)
						}
					}
					cmd := fmt.Sprintf(
						"sh -c 'test -s /etc/alpine-release && test \"$(uname -m)\" = x86_64 && printf \"INSULA_ALPINE_GUEST_OK_%s:%%s\\n\" \"$(cat /etc/alpine-release)\"'; printf 'INSULA_ALPINE_EXIT_%s:%%d\\n' \"$?\"\r",
						token, token)
					if _, err := console.Write([]byte(cmd)); err != nil {
						t.Fatal(err)
					}
					state = "command"
				}
			case "command":
				if alpineHasConsoleLine(transcript, guestLine) && alpineHasConsoleLine(transcript, exitLine) {
					state = "done"
				} else if alpineHasConsoleExit(transcript, "INSULA_ALPINE_EXIT_"+token+":") {
					t.Fatalf("guest command failed or Alpine version mismatch; serial=%s", serialPath)
				}
			}
		}
	}
	t.Logf("real Alpine guest command confirmed: %s", guestLine)

	// Request shutdown INSIDE the guest and require the transient domain to
	// disappear. An emergency 'virsh destroy' in Cleanup never passes this test.
	if _, err := console.Write([]byte("poweroff\r")); err != nil {
		t.Fatalf("cannot request guest poweroff: %v", err)
	}
	stopCtx, stopCancel := context.WithTimeout(ctx, 45*time.Second)
	defer stopCancel()
	for {
		exists, err := alpineDomainExists(stopCtx, uri, name)
		if err != nil {
			t.Fatalf("cannot verify stopped domain: %v", err)
		}
		if !exists {
			break
		}
		select {
		case chunk, ok := <-chunks:
			if !ok {
				chunks = nil
				continue
			}
			if _, err := serialFile.WriteString(chunk); err != nil {
				t.Fatal(err)
			}
		case <-stopCtx.Done():
			t.Fatalf("guest poweroff timed out; domain still present; serial=%s", serialPath)
		case <-time.After(500 * time.Millisecond):
		}
	}
	t.Log("INSULA_ALPINE_ISO_OK: boot, guest command, exit=0, graceful poweroff, absent domain")
}

func alpineISODomainXML(name, isoPath string, port int) string {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(isoPath))
	source := strings.ReplaceAll(escaped.String(), "'", "&apos;")
	return fmt.Sprintf(
		"<domain type='kvm'><name>%s</name>"+
			"<memory unit='MiB'>500</memory><currentMemory unit='MiB'>500</currentMemory><vcpu>1</vcpu>"+
			"<os><type arch='x86_64'>hvm</type><boot dev='cdrom'/></os>"+
			"<features><acpi/></features><on_poweroff>destroy</on_poweroff><devices>"+
			"<disk type='file' device='cdrom'><driver name='qemu' type='raw'/>"+
			"<source file='%s'/><target dev='sda' bus='sata'/><readonly/></disk>"+
			"<serial type='tcp'><source mode='bind' host='127.0.0.1' service='%d'/>"+
			"<protocol type='raw'/><target port='0'/></serial>"+
			"</devices></domain>", name, source, port)
}

func alpineValidateResources(raw string) error {
	var domain struct {
		Memory struct {
			Unit string `xml:"unit,attr"`
			Size int64 `xml:",chardata"`
		} `xml:"memory"`
		VCPU int `xml:"vcpu"`
	}
	if err := xml.Unmarshal([]byte(raw), &domain); err != nil {
		return fmt.Errorf("parse Libvirt XML: %w", err)
	}
	memoryKiB := domain.Memory.Size
	switch domain.Memory.Unit {
	case "KiB":
	case "MiB":
		memoryKiB *= 1024
	default:
		return fmt.Errorf("unexpected Libvirt memory unit %q", domain.Memory.Unit)
	}
	if domain.VCPU != 1 || memoryKiB != 500*1024 {
		return fmt.Errorf("Libvirt allocated wrong resources: vcpus=%d memory_kib=%d", domain.VCPU, memoryKiB)
	}
	return nil
}

// The Alpine Virtual serial shell appends an ANSI cursor-position request to
// its prompt; strip CSI escape codes before matching shell output.
var alpineANSI = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")

func alpineVisibleConsole(transcript string) string {
	return alpineANSI.ReplaceAllString(strings.ReplaceAll(transcript, "\r", ""), "")
}

func alpineHasConsoleLine(transcript, line string) bool {
	for _, part := range strings.Split(alpineVisibleConsole(transcript), "\n") {
		if strings.TrimSpace(part) == line {
			return true
		}
	}
	return false
}

func alpineHasConsoleExit(transcript, prefix string) bool {
	for _, part := range strings.Split(alpineVisibleConsole(transcript), "\n") {
		line := strings.TrimSpace(part)
		if strings.HasPrefix(line, prefix) && len(line) > len(prefix) {
			exitCode := strings.TrimPrefix(line, prefix)
			if regexp.MustCompile("^[0-9]+$").MatchString(exitCode) {
				return true
			}
		}
	}
	return false
}

func alpineDomainExists(ctx context.Context, uri, name string) (bool, error) {
	out, err := alpineVirsh(ctx, uri, "list", "--all", "--name")
	if err != nil {
		return false, fmt.Errorf("list Libvirt domains: %w: %s", err, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			return true, nil
		}
	}
	return false, nil
}

func alpineVirsh(ctx context.Context, uri string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "virsh", append([]string{"-c", uri}, args...)...)
	out, err := command.CombinedOutput()
	return string(out), err
}

func alpineDialConsole(ctx context.Context, port int) (net.Conn, error) {
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for {
		conn, err := net.DialTimeout("tcp4", address, 500*time.Millisecond)
		if err == nil {
			return conn, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("connect to %s: %w", address, ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func TestAlpineISOXMLContract(t *testing.T) {
	spec := alpineISODomainXML("insula-alpine-test", "/tmp/iso's & name.iso", 15999)
	for _, fragment := range []string{
		"<domain type='kvm'>", "<memory unit='MiB'>500</memory>",
		"<vcpu>1</vcpu>", "<boot dev='cdrom'/>", "iso&#39;s &amp; name.iso",
		"host='127.0.0.1'", "service='15999'", "<readonly/>",
	} {
		if !strings.Contains(spec, fragment) {
			t.Fatalf("Libvirt ISO XML missing %q: %s", fragment, spec)
		}
	}
	xmlFixture := "<domain><memory unit='KiB'>512000</memory><vcpu placement='static'>1</vcpu></domain>"
	if err := alpineValidateResources(xmlFixture); err != nil {
		t.Fatal(err)
	}
	if err := alpineValidateResources(strings.Replace(xmlFixture, "512000", "1024000", 1)); err == nil {
		t.Fatal("incorrect memory must fail")
	}
}

func TestAlpineISOSerialPromptWithANSI(t *testing.T) {
	// Captured from GitHub Actions: boot and root login succeeded, while ash
	// ended "localhost:~#" with CSI 6n, hiding it from the previous parser.
	captured := "\r\nlocalhost login: root\r\nWelcome to Alpine!\r\nlocalhost:~# \x1b[6n"
	prompt := regexp.MustCompile("(?m)(?:^|\n)[^\n]{0,100}# *$")
	if !prompt.MatchString(alpineVisibleConsole(captured)) {
		t.Fatalf("prompt must ignore ANSI control characters: %q", alpineVisibleConsole(captured))
	}
}

func TestAlpineISOTranscriptContract(t *testing.T) {
	const token = "abc012"
	echoed := "sh -c 'printf \"INSULA_ALPINE_GUEST_OK_abc012:3.24.4\"'\r\n"
	if alpineHasConsoleLine(echoed, "INSULA_ALPINE_GUEST_OK_abc012:3.24.4") {
		t.Fatal("an echoed command must not count as guest output")
	}
	actual := echoed + "INSULA_ALPINE_GUEST_OK_abc012:3.24.4\r\nINSULA_ALPINE_EXIT_abc012:0\r\n"
	if !alpineHasConsoleLine(actual, "INSULA_ALPINE_GUEST_OK_abc012:3.24.4") ||
		!alpineHasConsoleLine(actual, "INSULA_ALPINE_EXIT_abc012:0") {
		t.Fatal("authentic guest output not detected")
	}
}
