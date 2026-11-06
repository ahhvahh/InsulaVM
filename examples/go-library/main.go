// Example of embedding InsulaVM into another Go application.
// This program is intentionally separate from the insulavm CLI.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ahhvahh/InsulaVM/insulavm"
)

func main() {
	engine, err := insulavm.New(os.Getenv("INSULAVM_CONFIG"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	// Another Go service can also construct the manifest directly in memory.
	// No YAML file and no separate executable are needed for this call.
	m := &insulavm.Manifest{
		Repository: insulavm.Repository{Name: "example-repo"},
		Action:     "checks",
		Workspace:  "/app/repo-workspace",
		Pipelines: []insulavm.Pipeline{{
			Name: "validation",
			Jobs: []insulavm.Job{{
				Name:     "check-version",
				Commands: []string{"uname -a", "pwd"},
			}},
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := engine.Run(ctx, insulavm.RunRequest{
		Manifest:   m,
		Host:       os.Getenv("INSULAVM_HOST"), // Existing running VM
		User:       os.Getenv("INSULAVM_USER"),
		PrivateKey: os.Getenv("INSULAVM_KEY_PATH"),
		KnownHosts: os.Getenv("INSULAVM_KNOWN_HOSTS"),
		Output:     os.Stdout, // live SSH job output, also recorded to disk
	})
	if result != nil {
		fmt.Println("log directory:", result.LogDirectory)
		fmt.Println("completed jobs:", result.CompletedJobs)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "action failed:", err)
		os.Exit(1)
	}
}
