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
        case "test":
            return testCommand(args[1:])
		case "run":
			return runAction(args[1:])
		case "import":
			return importCommand(args[1:])
		case "help", "-h", "--help":
			fmt.Println("       insula test doctor|download|install|boot|ssh|shell|iso|verify|cycle")
			fmt.Println("Usage: insula run -config config.yaml -manifest insula.yaml")
			fmt.Println("       insula import -config config.yaml -host 192.0.2.10:22 -source /host/project")
			fmt.Println("       insula config check -config config.yaml")
			fmt.Println("       insula vm start -config config.yaml -profile debian-minimal [-memory-mib 2048] [-vcpus 2]")
			fmt.Println("       insula vm list -config config.yaml")
			fmt.Println("       insula vm status -config config.yaml -id UUIDv7")
			fmt.Println("       insula vm shell -config config.yaml -id UUIDv7 [-directory InsulaVM]")
			fmt.Println("       insula vm stop -config config.yaml -id UUIDv7 [-grace-timeout 30s] [-force-after-timeout=true]")
			fmt.Println("       insula vm start-iso -profile debian-go [-iso debian-go-live.iso]")
			fmt.Println("       insula vm console -id UUIDv7")
			fmt.Println("       insula vm stop-all -config config.yaml [-dry-run]")
			fmt.Println("       insula vm freeze -id UUIDv7 -name debian13-tests [-shutdown-timeout 3m]")
			fmt.Println("       insula vm exec -id UUIDv7 -command \"id -u\" [-sudo]")
			fmt.Println("       insula vm import -id UUIDv7 -source /host/projeto")
			fmt.Println("       insula vm deploy -config config.yaml -id UUIDv7 -source pacote.tar.gz -script instalar.sh [-extract] [-sudo]")
			fmt.Println("       insula profile list -config config.yaml")
			fmt.Println("       insula profile show -config config.yaml -name debian-go")
			fmt.Println("       insula profile register -config config.yaml -name debian-go -image installed.qcow2 -memory-mib 1024 -vcpus 1 -disk-gb 8")
			fmt.Println("       insula profile clone -source debian-go -name debian-go-copy")
			fmt.Println("       insula profile create-from-iso -name debian-13 -iso /isos/debian-13-amd64-netinst.iso -memory-mib 2048 -vcpus 2 -disk-gb 12")
			fmt.Println("       insula vm export-iso -id UUIDv7 -name debian-live [-prepare-live-boot]")
			fmt.Println("       insula profile export-iso -name debian-go")
			return nil
		}
	}
	return errors.New("usage: insula <config|profile|vm|test|run|import|help>; use insula run for pipelines")
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
		localPathForCLI(c.Storage.Profiles), localPathForCLI(c.Storage.Runs), localPathForCLI(c.Storage.Logs), c.Virtualization.Provider, c.Guest.Destination, c.SSH.User)
	return nil
}

func profileCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("expected profile list, show, register, clone, create-from-iso or export-iso")
	}
	cmd := args[0]
	fs := flag.NewFlagSet("profile "+cmd, flag.ContinueOnError)
	cfgPath := fs.String("config", "config.yaml", "global config")
	name := fs.String("name", "", "profile name")
	sourceProfile := fs.String("source", "", "source profile name for cloning")
	image := fs.String("image", "", "fully installed QCOW2 source path")
	memory := fs.Int("memory-mib", 0, "guest RAM (MiB)")
	cpus := fs.Int("vcpus", 0, "guest vCPUs")
	disk := fs.Int("disk-gb", 0, "virtual disk capacity in GiB")
	distro := fs.String("os", "debian", "OS label")
	iso := fs.String("iso", "", "local installer ISO path for create-from-iso; other commands store metadata only")
	outputISO := fs.String("output", "", "optional Live ISO destination (absolute or relative to current directory)")
	installTimeout := fs.Duration("install-timeout", 90*time.Minute, "Debian ISO installer maximum duration")
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
		fmt.Printf("Profile registered: %s\n", localPathForCLI(filepath.Join(c.Storage.Profiles, p.Name, "profile.yaml")))
	case "clone":
		if *sourceProfile == "" || *name == "" { return errors.New("profile clone requires -source and -name") }
		p, err := engine.CloneProfile(context.Background(),*sourceProfile,*name)
		if err != nil { return err }
		fmt.Printf("PROFILE CLONE OK: source=%s name=%s\n",*sourceProfile,p.Name)
	case "create-from-iso":
		if *name == "" || *iso == "" { return errors.New("profile create-from-iso requires -name and -iso") }
		c := engine.Configuration()
		ram, cores, size := *memory, *cpus, *disk
		if ram == 0 { ram = 2048 }
		if cores == 0 { cores = 2 }
		if size == 0 { size = c.Virtualization.DefaultDiskGB }
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		result, err := engine.CreateProfileFromISO(ctx, insulavm.CreateProfileFromISORequest{
			Name:*name, ISOPath:*iso, MemoryMiB:ram, VCPUs:cores,
			DiskGB:size, Timeout:*installTimeout,
			OnProgress:func(p insulavm.ProfileISOProgress) {
				fmt.Fprintf(os.Stderr, "[InsulaVM][iso:%s] %s\n",p.Stage,p.Message)
			},
		})
		if err != nil { return err }
		fmt.Printf("PROFILE ISO OK: name=%s image=%s sha256=%s\n",result.Profile.Name,localPathForCLI(result.ImagePath),result.ISOChecksumSHA256)
	case "export-iso":
		if *name == "" { return errors.New("profile export-iso requires -name") }
		path, err := engine.ExportProfileLiveISOTo(context.Background(),*name,*outputISO)
		if err != nil { return err }
		fmt.Printf("LIVE ISO OK: %s\n",localPathForCLI(path))
	default:
		return errors.New("expected profile list, show, register, clone or export-iso")
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
	storageRoot := engine.Configuration().Storage.Root
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
			fmt.Fprintf(os.Stderr,"[InsulaVM][vm:%s] %s\n",event.Stage,localMessageForCLI(event.Message,storageRoot))
		},
	})
	if result != nil && result.LogDirectory != "" {
		fmt.Println("Logs:", localPathForCLI(result.LogDirectory))
	}
	if err != nil {
		return presentCLIError(err,storageRoot)
	}
	fmt.Println("InsulaVM: all jobs completed")
	return nil
}
