// InsulaVM CLI: execute a manifest on an already running Debian VM.
// VM provisioning and repository transfer are planned in separate modules.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/ahhvahh/InsulaVM/pkg/executor"
	"github.com/ahhvahh/InsulaVM/pkg/manifest"
	"github.com/ahhvahh/InsulaVM/pkg/storage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "InsulaVM:", err)
		os.Exit(1)
	}
}

func run() error {
	manifestPath := flag.String("manifest", "insula.yaml", "path to insula.yaml")
	logRoot := flag.String("logs-root", os.Getenv("INSULA_LOG_ROOT"), "host directory for persistent job logs")
	host := flag.String("host", "", "existing Debian VM SSH address (host:port)")
	user := flag.String("user", "", "SSH account on the Debian VM")
	key := flag.String("key", "", "path to SSH private key")
	knownHosts := flag.String("known-hosts", "", "trusted SSH known_hosts file")
	timeout := flag.Duration("command-timeout", 30*time.Minute, "per-command timeout")
	flag.Parse()

	if *host == "" || *user == "" || *key == "" || *knownHosts == "" || *logRoot == "" {
		return errors.New("-host, -user, -key, -known-hosts and -logs-root are required")
	}
	if *timeout <= 0 {
		return errors.New("-command-timeout must be positive")
	}
	m, err := manifest.ReadFile(*manifestPath)
	if err != nil {
		return err
	}
	r, err := storage.NewRun(*logRoot, m.Repository.Name, m.GroupName(), time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Logs:", r.Directory)
	client, err := executor.Dial(executor.DialConfig{
		Address: *host, User: *user, PrivateKeyPath: *key,
		KnownHostsPath: *knownHosts,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	exec := &executor.Executor{
		Client: client, Workspace: m.Workspace,
		Console: os.Stdout, CommandTimeout: *timeout,
	}
	runJob := func(name string, commands []string, allowFailure bool) error {
		log, err := r.OpenJob(name)
		if err != nil {
			return err
		}
		runErr := exec.Run(ctx, name, commands, allowFailure, log)
		closeErr := log.Close()
		return errors.Join(runErr, closeErr)
	}
	if len(m.Setup) != 0 {
		if err := runJob("setup", m.Setup, false); err != nil {
			return err
		}
	}
	for _, pipeline := range m.Pipelines {
		for _, job := range pipeline.Jobs {
			if err := runJob(job.Name, job.Commands, job.AllowFailure); err != nil {
				return fmt.Errorf("pipeline %s: %w", pipeline.Name, err)
			}
		}
	}
	fmt.Fprintln(os.Stdout, "InsulaVM: all jobs completed")
	return nil
}
