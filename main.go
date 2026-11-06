// Command InsulaVM exposes a CLI over the reusable insulavm Go library.
// Import github.com/ahhvahh/InsulaVM/insulavm in Go applications instead of
// spawning this binary when direct in-process orchestration is desired.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/ahhvahh/InsulaVM/insulavm"
	"github.com/ahhvahh/InsulaVM/pkg/profile"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "InsulaVM:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "config":
			return configCommand(args[1:])
		case "profile":
			return profileCommand(args[1:])
		case "vm":
			return vmCommand(args[1:])
		case "run":
			return runAction(args[1:])
		case "import":
			return importCommand(args[1:])
		case "help", "-h", "--help":
			fmt.Println("Usage: insula [run] -config config.yaml -manifest insula.yaml")
			fmt.Println("       insula import -config config.yaml -host 192.0.2.10:22 -source /host/project")
			fmt.Println("       insula config check -config config.yaml")
			fmt.Println("       insula vm start -config config.yaml -profile debian-minimal")
			fmt.Println("       insula vm list -config config.yaml")
			fmt.Println("       insula vm show -config config.yaml -id UUIDv7")
			fmt.Println("       insula vm status -config config.yaml -id UUIDv7")
			fmt.Println("       insula vm stop -config config.yaml -id UUIDv7")
			fmt.Println("       insula vm exec -id UUIDv7 -command \"id -u\" [-sudo]")
			fmt.Println("       insula vm import -id UUIDv7 -source /host/projeto")
			fmt.Println("       insula profile list -config config.yaml")
			fmt.Println("       insula profile show -config config.yaml -name debian-go")
			fmt.Println("       insula profile register -config config.yaml -name debian-go -image installed.qcow2 -memory-mib 1024 -vcpus 1 -disk-gb 8")
			return nil
		}
	}
	return runAction(args)
}

func configCommand(args []string) error {
	if len(args) < 1 || args[0] != "check" {
		return errors.New("usage: insula config check [-config config.yaml]")
	}
	fs := flag.NewFlagSet("config check", flag.ContinueOnError)
	filename := fs.String("config", "config.yaml", "global config")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	engine, err := insulavm.New(*filename)
	if err != nil {
		return err
	}
	c := engine.Configuration()
	fmt.Printf("Valid: profiles=%s runs=%s logs=%s provider=%s guest_destination=%s ssh_user=%s\n",
		c.Storage.Profiles, c.Storage.Runs, c.Storage.Logs, c.Virtualization.Provider, c.Guest.Destination, c.SSH.User)
	return nil
}

func profileCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("expected profile list, show or register")
	}
	cmd := args[0]
	fs := flag.NewFlagSet("profile "+cmd, flag.ContinueOnError)
	cfgPath := fs.String("config", "config.yaml", "global config")
	name := fs.String("name", "", "profile name")
	image := fs.String("image", "", "fully installed QCOW2 source path")
	memory := fs.Int("memory-mib", 0, "guest RAM (MiB)")
	cpus := fs.Int("vcpus", 0, "guest vCPUs")
	disk := fs.Int("disk-gb", 0, "virtual disk capacity in GiB")
	distro := fs.String("os", "debian", "OS label")
	iso := fs.String("iso", "", "original ISO path (metadata only)")
	address := fs.String("address", "", "existing guest SSH address (optional)")
	user := fs.String("user", "", "guest SSH user (optional)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected profile arguments")
	}
	engine, err := insulavm.New(*cfgPath)
	if err != nil {
		return err
	}
	switch cmd {
	case "list":
		names, err := engine.ListProfiles()
		if err != nil {
			return err
		}
		if len(names) == 0 {
			fmt.Println("No registered profiles")
		}
		for _,n := range names {
			fmt.Println(n)
		}
	case "show":
		p, err := engine.GetProfile(*name)
		if err != nil {
			return err
		}
		fmt.Printf("name=%s os=%s memory_mib=%d vcpus=%d disk_gb=%d version=%s image=%s\n",
			p.Name, p.OS, p.MemoryMiB, p.VCPUs, p.DiskGB, p.Version, p.Image)
	case "register":
		if *name == "" || *image == "" {
			return errors.New("profile register requires -name and -image")
		}
		p := insulavm.Profile{
			Name: *name,
			OS: *distro,
			MemoryMiB: *memory,
			VCPUs: *cpus,
			DiskGB: *disk,
			ISO: *iso,
			SSH: profile.SSH{Address: *address, User: *user},
		}
		if err := engine.RegisterProfile(p, *image); err != nil {
			return err
		}
		c := engine.Configuration()
		fmt.Printf("Profile registered: %s\n", filepath.Join(c.Storage.Profiles, p.Name, "profile.yaml"))
	default:
		return errors.New("expected profile list, show or register")
	}
	return nil
}

func runAction(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfgPath := fs.String("config", "config.yaml", "global config path")
	manifestPath := fs.String("manifest", "insula.yaml", "job manifest")
	logsOverride := fs.String("logs-root", "", "override global log directory")
	hostOverride := fs.String("host", "", "connect to an existing guest; do not start a VM")
	userOverride := fs.String("user", "", "override SSH user")
	keyOverride := fs.String("key", "", "override SSH private key")
	hostsOverride := fs.String("known-hosts", "", "override SSH known_hosts")
	timeout := fs.Duration("command-timeout", 30*time.Minute, "per-command timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected run arguments")
	}
	if *timeout <= 0 {
		return errors.New("command-timeout must be positive")
	}
	engine, err := insulavm.New(*cfgPath)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	result, err := engine.Run(ctx, insulavm.RunRequest{
		ManifestPath: *manifestPath,
		LogRoot: *logsOverride,
		Host: *hostOverride,
		User: *userOverride,
		PrivateKey: *keyOverride,
		KnownHosts: *hostsOverride,
		CommandTimeout: *timeout,
		Output: os.Stdout,
		OnVMProgress: func(event insulavm.VMProgress){
			fmt.Fprintf(os.Stderr,"[InsulaVM][vm:%s] %s\n",event.Stage,event.Message)
		},
	})
	if result != nil && result.LogDirectory != "" {
		fmt.Println("Logs:", result.LogDirectory)
	}
	if err != nil {
		return err
	}
	fmt.Println("InsulaVM: all jobs completed")
	return nil
}
