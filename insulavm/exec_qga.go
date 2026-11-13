package insulavm

import (
 "context"
 "errors"
 "io"
 "strings"

 vmtransport "github.com/ahhvahh/InsulaVM/pkg/vm"
)

// execVMQGA executes via Libvirt's private guest-agent channel, without
// resolving an IP, SSH identity, or profile workspace.
func (e *Engine) execVMQGA(ctx context.Context, id string, req ExecRequest) (*ExecResult, error) {
	if req.Sudo {
		return nil, errors.New("-sudo is not supported with QGA: commands run with the qemu-ga service identity (usually root)")
	}
	if req.CommandTimeout < 0 {
		return nil, errors.New("command timeout must not be negative")
	}
	command := req.Command
	if req.Workspace != "" {
		if !strings.HasPrefix(req.Workspace, "/") || strings.ContainsAny(req.Workspace, "\x00\r\n") {
			return nil, errors.New("workspace must be an absolute guest path")
		}
		command = "cd " + quoteGuest(req.Workspace) + " && " + command
	}

	// Keep QGA's captured bytes in the result and optionally copy them to
	// caller-provided writers. Execution of a command must not silently
	// persist potentially sensitive stdout/stderr in storage.logs.
	// The QGA protocol makes these bytes available only after exit.
	result := &ExecResult{VMID: id}
	output, execErr := vmtransport.ExecuteQGA(ctx, e.config.Virtualization.LibvirtURI, id, command, req.CommandTimeout)
	result.Stdout, result.Stderr, result.ExitCode = output.Stdout, output.Stderr, output.ExitCode
	result.StdoutTruncated, result.StderrTruncated = output.StdoutTruncated, output.StderrTruncated

	var outputErr error
	if req.Output != nil {
		outputErr = errors.Join(outputErr, writeExact(req.Output, output.Stdout))
	}
	if req.ErrorOutput != nil {
		outputErr = errors.Join(outputErr, writeExact(req.ErrorOutput, output.Stderr))
	}
	return result, errors.Join(execErr, outputErr)
}

func writeExact(w io.Writer, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
