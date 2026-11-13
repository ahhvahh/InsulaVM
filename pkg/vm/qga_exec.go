package vm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// QGAExecOutput contains the native bytes and exit status from guest-exec.
// Output is returned only after the guest process has exited; QGA does not
// provide a streaming guest-exec interface.
type QGAExecOutput struct {
	Stdout          []byte
	Stderr          []byte
	ExitCode        *int
	Signal          *int
	StdoutTruncated bool
	StderrTruncated bool
}

// QGAExitError reports a guest process failure, distinct from a Libvirt
// transport failure. The output is still returned to the caller.
type QGAExitError struct {
	Code int
}

func (e *QGAExitError) Error() string {
	return fmt.Sprintf("QGA guest command exited with code %d", e.Code)
}

type qgaEnvelope struct {
	Return json.RawMessage `json:"return"`
	Error  *struct {
		Class string `json:"class"`
		Desc  string `json:"desc"`
	} `json:"error"`
}

// qgaCall sends structured JSON as one argument, never through a host shell.
// A per-request Libvirt timeout prevents a disconnected guest agent from
// blocking indefinitely, while ctx bounds the complete execution.
func qgaCall(ctx context.Context, uri, uuid, operation string, arguments any, reply any) error {
	request := struct {
		Execute   string `json:"execute"`
		Arguments any    `json:"arguments,omitempty"`
	}{Execute: operation, Arguments: arguments}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "virsh", "-c", uri, "qemu-agent-command",
		uuid, "--timeout", "5", string(payload))
	raw, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("QGA %s interrupted (the guest process may continue): %w", operation, ctx.Err())
		}
		return fmt.Errorf("QGA %s via Libvirt: %w (verify guest-agent installation and Virtio channel)", operation, err)
	}
	var envelope qgaEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode QGA %s reply: %w", operation, err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("QGA %s rejected: %s: %s", operation, envelope.Error.Class, envelope.Error.Desc)
	}
	if len(envelope.Return) == 0 {
		return fmt.Errorf("QGA %s reply contains no return value", operation)
	}
	if err := json.Unmarshal(envelope.Return, reply); err != nil {
		return fmt.Errorf("decode QGA %s result: %w", operation, err)
	}
	return nil
}

// ExecuteQGA runs one trusted shell command in an existing VM over its
// Libvirt virtio-serial guest-agent channel. It requires no guest IP or SSH.
// The command runs with the qemu-ga service identity (typically root).
// Cancellation stops waiting on the host but does not guarantee the guest
// process is terminated.
func ExecuteQGA(ctx context.Context, uri, uuid, command string, timeout time.Duration) (QGAExecOutput, error) {
	var result QGAExecOutput
	if ctx == nil {
		return result, errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if uri == "" || uuid == "" || strings.TrimSpace(command) == "" || strings.ContainsRune(command, 0) {
		return result, errors.New("Libvirt URI, VM UUID and non-empty command are required")
	}
	if timeout < 0 {
		return result, errors.New("QGA command timeout cannot be negative")
	}
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var started struct {
		PID int `json:"pid"`
	}
	err := qgaCall(ctx, uri, uuid, "guest-exec", map[string]any{
		"path": "/bin/sh", "arg": []string{"-c", command}, "capture-output": true,
	}, &started)
	if err != nil {
		return result, err
	}
	if started.PID <= 0 {
		return result, fmt.Errorf("QGA guest-exec returned invalid PID %d", started.PID)
	}

	type guestStatus struct {
		Exited          bool   `json:"exited"`
		ExitCode        *int   `json:"exitcode"`
		Signal          *int   `json:"signal"`
		OutData         string `json:"out-data"`
		ErrData         string `json:"err-data"`
		OutTruncated    bool   `json:"out-truncated"`
		ErrTruncated    bool   `json:"err-truncated"`
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status guestStatus
		err := qgaCall(ctx, uri, uuid, "guest-exec-status", map[string]any{"pid": started.PID}, &status)
		if err != nil {
			return result, fmt.Errorf("QGA guest process %d status (process may continue): %w", started.PID, err)
		}
		if status.Exited {
			result.ExitCode = status.ExitCode
			result.Signal = status.Signal
			result.StdoutTruncated = status.OutTruncated
			result.StderrTruncated = status.ErrTruncated
			result.Stdout, err = base64.StdEncoding.DecodeString(status.OutData)
			if err != nil {
				return result, fmt.Errorf("decode QGA stdout: %w", err)
			}
			result.Stderr, err = base64.StdEncoding.DecodeString(status.ErrData)
			if err != nil {
				return result, fmt.Errorf("decode QGA stderr: %w", err)
			}
			if result.StdoutTruncated || result.StderrTruncated {
				return result, errors.New("QGA captured output was truncated by the guest agent")
			}
			if status.Signal != nil {
				return result, fmt.Errorf("QGA guest process terminated by signal %d", *status.Signal)
			}
			if status.ExitCode == nil {
				return result, errors.New("QGA guest process exited without exit code")
			}
			if *status.ExitCode != 0 {
				return result, &QGAExitError{Code: *status.ExitCode}
			}
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("QGA guest process %d timed out; it may continue inside the VM: %w", started.PID, ctx.Err())
		case <-ticker.C:
		}
	}
}

// QGAPing verifies that the guest agent is responsive over the registered
// domain's virtio channel, independently of DHCP and SSH.
func QGAPing(ctx context.Context, uri, uuid string) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if uri == "" || uuid == "" {
		return errors.New("Libvirt URI and VM UUID are required")
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var reply struct{}
	if err := qgaCall(pingCtx, uri, uuid, "guest-ping", nil, &reply); err != nil {
		return err
	}
	return nil
}

