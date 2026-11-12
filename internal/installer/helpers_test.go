package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckISORecognizesISO9660AndRejectsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "debian.iso")
	data := make([]byte, 16*2048+6)
	copy(data[16*2048+1:], "CD001")
	if err := os.WriteFile(valid, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckISO(valid); err != nil {
		t.Fatalf("valid ISO rejected: %v", err)
	}

	invalid := filepath.Join(dir, "invalid.iso")
	if err := os.WriteFile(invalid, make([]byte, len(data)), 0600); err != nil {
		t.Fatal(err)
	}
	truncated := filepath.Join(dir, "truncated.iso")
	if err := os.WriteFile(truncated, []byte("CD001"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"bad signature", invalid, "CD001"},
		{"truncated", truncated, "read ISO9660"},
		{"missing file", filepath.Join(dir, "missing.iso"), "no such file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckISO(tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CheckISO(%q) = %v, want %q", tc.path, err, tc.want)
			}
		})
	}
}

func TestSHA256FileAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload")
	payload := []byte("insula installer checksum")
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	got, err := SHA256File(context.Background(), path)
	if err != nil || got != hex.EncodeToString(digest[:]) {
		t.Fatalf("checksum = %q, error = %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SHA256File(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not propagated: %v", err)
	}
	if _, err := SHA256File(context.Background(), path+"-missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing input accepted: %v", err)
	}
}

func TestRandomHexProducesDecodableIdentifier(t *testing.T) {
	got, err := RandomHex(16)
	if err != nil || len(got) != 32 {
		t.Fatalf("invalid random identifier: %q, %v", got, err)
	}
	if _, err := hex.DecodeString(got); err != nil {
		t.Fatalf("identifier not hex encoded: %v", err)
	}
}

func TestHashPasswordChecksCommandAndFormat(t *testing.T) {
	bin := t.TempDir()
	command := filepath.Join(bin, "openssl")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	scripts := []struct {
		name   string
		body   string
		want   string
	}{
		{
			"success",
			"test \"$1\" = passwd && test \"$2\" = -6 && test \"$3\" = -stdin\nread -r secret\ntest \"$secret\" = expected\nprintf '%s\\n' '$6$mock$hash'",
			"$6$mock$hash",
		},
		{"bad format", "printf '%s\\n' 'not-a-sha512-crypt'", "SHA-512"},
		{"tool failure", "echo rejected >&2; exit 41", "hash ephemeral installer password"},
	}
	for _, tc := range scripts {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(command, []byte("#!/bin/sh\nset -eu\n"+tc.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			got, err := HashPassword(context.Background(), "expected")
			if tc.name == "success" {
				if err != nil || got != tc.want {
					t.Fatalf("hash = %q, err = %v", got, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("failure not handled: hash=%q err=%v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestRenderPreseedUsesLockedRootAndExplicitSudoPolicy(t *testing.T) {
	for _, tc := range []struct {
		sudo bool
		wantSudo bool
	}{
		{false, false},
		{true, true},
	} {
		got := RenderPreseed("insula", "$6$mock$hash", tc.sudo)
		for _, required := range []string{
			"d-i passwd/root-login boolean false",
			"d-i passwd/user-password-crypted password $6$mock$hash",
			"d-i debian-installer/exit/poweroff boolean true",
		} {
			if !strings.Contains(got, required) {
				t.Fatalf("missing preseed directive %q", required)
			}
		}
		if strings.Contains(got, "NOPASSWD:ALL") != tc.wantSudo {
			t.Fatalf("incorrect sudo policy (enabled=%t)", tc.sudo)
		}
	}
}

func TestTailOutputBoundsDiagnostic(t *testing.T) {
	if got := TailOutput("  short message \n"); got != "short message" {
		t.Fatalf("unexpected trimming: %q", got)
	}
	input := strings.Repeat("x", 3200) + "last-line"
	got := TailOutput(input)
	if len(got) != 3003 || !strings.HasPrefix(got, "...") || !strings.HasSuffix(got, "last-line") {
		t.Fatalf("diagnostic not bounded: length=%d", len(got))
	}
}
