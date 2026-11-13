package insulavm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestExecVMQGAConcurrentInstancesUseTheirOwnUUID(t *testing.T) {
	engine := testEngine(t, "")
	idA := "019a5d11-1234-7123-8123-123456789abc"
	idB := "019a5d11-1234-7123-8123-123456789abe"
	registerQGANoIPVM(t, engine, idA)
	registerQGANoIPVM(t, engine, idB)
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"case \"$3\" in\n" +
		" list) printf '%s\\n%s\\n' '" + idA + "' '" + idB + "' ;;\n" +
		" domstate) printf '%s\\n' running ;;\n" +
		" qemu-agent-command)\n" +
		"  case \"$7\" in\n" +
		"  *guest-exec-status*)\n" +
		"   case \"$4\" in\n" +
		"   '" + idA + "') printf '%s\\n' '{\"return\":{\"exited\":true,\"exitcode\":0,\"out-data\":\"QQo=\"}}' ;;\n" +
		"   '" + idB + "') printf '%s\\n' '{\"return\":{\"exited\":true,\"exitcode\":0,\"out-data\":\"Qgo=\"}}' ;;\n" +
		"   *) exit 25 ;; esac ;;\n" +
		"  *guest-exec*) printf '%s\\n' '{\"return\":{\"pid\":222}}' ;;\n" +
		"  *) exit 26 ;; esac ;;\n" +
		" *) exit 27 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "virsh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, test := range []struct {
		id string
		want string
	}{{idA, "A\n"}, {idB, "B\n"}} {
		wg.Add(1)
		go func(id, want string) {
			defer wg.Done()
			result, err := engine.ExecVM(context.Background(), ExecRequest{VMID:id, Command:"printf identity", QGA:true})
			if err != nil {
				errs <- err
				return
			}
			if result == nil || string(result.Stdout) != want ||
				!strings.Contains(result.LogDirectory, id) {
				errs <- fmt.Errorf("QGA identity isolation failed: vm=%s result=%+v", id, result)
			}
		}(test.id, test.want)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
