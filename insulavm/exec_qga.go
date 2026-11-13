package insulavm

import (
 "context"
 "encoding/base64"
 "errors"
 "fmt"
 "io"
 "strings"
 "time"
 "unicode/utf8"

 "github.com/ahhvahh/InsulaVM/pkg/executor"
 "github.com/ahhvahh/InsulaVM/pkg/storage"
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

	// The QGA protocol delivers captured bytes after process exit. An
	// explicit Log opt-in stores a separate diagnostic command.log on the host.
	result := &ExecResult{VMID: id}
	var log executor.Recorder
	var closeLog func()error
	if req.Log{
	 run,err:=storage.NewRun(e.config.Storage.Logs,"vm-"+id,"exec",time.Now())
	 if err!=nil{return result,err}
	 result.LogDirectory=run.Directory
	 job,err:=run.OpenJob("command")
	 if err!=nil{return result,err}
	 if err:=job.Request(req.Command);err!=nil{return result,errors.Join(err,job.Close())}
	 log=job
	 closeLog=job.Close
	}
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
	if log!=nil{
		for _,part:=range []struct{name string;data []byte}{
		 {"stdout",output.Stdout},{"stderr",output.Stderr},
		}{
		 if len(part.data)==0{continue}
		 if utf8.Valid(part.data)&&!strings.ContainsRune(string(part.data),0){
		  outputErr=errors.Join(outputErr,log.Response(part.name+": "+string(part.data)))
		 }else{
		  outputErr=errors.Join(outputErr,log.Response(part.name+" (base64): "+base64.StdEncoding.EncodeToString(part.data)))
		 }
		}
		if output.ExitCode!=nil{
		 outputErr=errors.Join(outputErr,log.Response(fmt.Sprintf("exit_code: %d",*output.ExitCode)))
		}
		outputErr=errors.Join(outputErr,closeLog())
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
