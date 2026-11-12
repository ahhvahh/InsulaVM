// Package insulavm exposes the InsulaVM execution engine as a Go library.
//
// Import github.com/ahhvahh/InsulaVM/insulavm from another Go module.
// The CLI is a separate consumer of this package. This API is experimental:
// no stable v1 tag or backwards-compatibility promise has been published.
//
// An existing Linux guest must have SSH and the workspace configured.
// Automatic VM creation requires a previously installed QCOW2 profile and
// QEMU/Libvirt on the host. OS installation, Cloud-Init provisioning and
// arbitrary host file/directory imports are supported. Git checkout is not yet automated.
package insulavm

import (
 shelltransport "github.com/ahhvahh/InsulaVM/internal/shell"
 "os"
 "sync"
	"context"
	"errors"
	"fmt"
	"io"
 "path/filepath"
	"time"

	"github.com/ahhvahh/InsulaVM/pkg/config"
	"github.com/ahhvahh/InsulaVM/pkg/executor"
	"github.com/ahhvahh/InsulaVM/pkg/manifest"
	"github.com/ahhvahh/InsulaVM/pkg/profile"
	"github.com/ahhvahh/InsulaVM/pkg/registry"
	"github.com/ahhvahh/InsulaVM/pkg/storage"
	"github.com/ahhvahh/InsulaVM/pkg/vm"
	"golang.org/x/crypto/ssh"
)

// Public aliases let consumers construct manifests without importing the
// lower-level parser package. The underlying structures are shared.
type Manifest = manifest.Manifest
type Repository = manifest.Repository
type Pipeline = manifest.Pipeline
type Job = manifest.Job
type ImportSpec = manifest.ImportSpec
type SSHOptions = manifest.SSHOptions
type Profile = profile.Profile
// VMInfo is persisted in the shared SQLite registry.
type VMInfo = registry.Entry
// VMLiveInfo combines SQLite history and the current Libvirt domain state.
type VMLiveInfo = vm.LiveInfo
// VMProgress represents a synchronous stage update during startup.
type VMProgress = vm.Progress

// Engine holds immutable host configuration. Multiple Run calls may use the
// same Engine concurrently; each run creates its own log directory and VM.
type Engine struct {
	config config.Config
	configDir string
 shellMu sync.Mutex
 shells map[string]*shelltransport.Session
 shellsClosed bool
}

// New reads a global config.yaml file without creating any VM or directory.
func New(configPath string) (*Engine, error) {
	cfg, err := config.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	absolute,err:=filepath.Abs(configPath)
 if err!=nil{return nil,err}
 return &Engine{config: *cfg,configDir:filepath.Dir(absolute)}, nil
}

// Configuration returns a copy of the loaded global settings.
func (e *Engine) Configuration() config.Config {
	return e.config
}

// ListProfiles lists recognized profile directory names.
func (e *Engine) ListProfiles() ([]string, error) {
	return profile.List(e.config.Storage.Profiles)
}

// GetProfile loads metadata for a named profile without starting a VM.
func (e *Engine) GetProfile(name string) (*Profile, error) {
	return profile.Read(e.config.Storage.Profiles, name)
}

// RegisterProfile copies a fully installed QCOW2 image to the profile store.
// It does not install an OS. Existing profile names are never overwritten.
func (e *Engine) RegisterProfile(p Profile, sourceQCOW2 string) error {
 if p.Guest.Destination=="" && p.Workspace.Destination=="" {p.Guest.Destination=e.config.Guest.Destination}
	if p.DiskGB == 0 {
		p.DiskGB = e.config.Virtualization.DefaultDiskGB
	}
	return profile.Register(e.config.Storage.Profiles, p, sourceQCOW2)
}

// GuestDestinationForProfile resolves the guest directory for a profile.
// Existing workspace.destination takes precedence over guest.destination.
func (e *Engine) GuestDestinationForProfile(name string)(string,error){
 if e==nil{return "",errors.New("engine is required")}
 if name==""{return e.config.Guest.Destination,nil}
 p,err:=e.GetProfile(name)
 if errors.Is(err,os.ErrNotExist){return e.config.Guest.Destination,nil}
 if err!=nil{return "",fmt.Errorf("load profile %s guest destination: %w",name,err)}
 return p.GuestDestination(e.config.Guest.Destination),nil
}

// StartVM creates a disposable Libvirt domain from the named QCOW2 profile.
// The caller MUST invoke instance.Cleanup(ctx) once finished, even on error.
func (e *Engine) StartVM(ctx context.Context, name string) (*vm.Instance, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}
	p, err := e.GetProfile(name)
	if err != nil {
		return nil, err
	}
	if err := e.config.EnsureDirectories(); err != nil {
		return nil, err
	}
	return vm.Start(ctx, &e.config, p)
}

// GetVM loads a VM by its persistent UUIDv7 identifier.
func (e *Engine) GetVM(ctx context.Context,id string)(VMInfo,error){
 if ctx==nil{return VMInfo{},errors.New("context is required")}
 store,err:=registry.Open(e.config.Storage.Database)
 if err!=nil{return VMInfo{},err}
 defer store.Close()
 return store.Get(ctx,id)
}

// ListVMs lists historical and currently registered VM instances.
func (e *Engine) ListVMs(ctx context.Context)([]VMInfo,error){
 if ctx==nil{return nil,errors.New("context is required")}
 store,err:=registry.Open(e.config.Storage.Database)
 if err!=nil{return nil,err}
 defer store.Close()
 return store.List(ctx)
}

// ListVMStatus checks registered instances against the actual Libvirt domains.
// It never silently rewrites SQLite and never treats a failed Libvirt query
// as proof that a VM is powered off.
func (e *Engine) ListVMStatus(ctx context.Context)([]VMLiveInfo,error){
 if ctx==nil{return nil,errors.New("context is required")}
 records,err:=e.ListVMs(ctx)
 if err!=nil{return nil,err}
 return vm.InspectRegistered(ctx,e.config.Virtualization.LibvirtURI,records)
}

// GetVMStatus obtains the persisted metadata and the actual Libvirt state.
func (e *Engine) GetVMStatus(ctx context.Context,id string)(VMLiveInfo,error){
 record,err:=e.GetVM(ctx,id)
 if err!=nil{return VMLiveInfo{},err}
 statuses,err:=vm.InspectRegistered(ctx,e.config.Virtualization.LibvirtURI,[]registry.Entry{record})
 if err!=nil{return VMLiveInfo{},err}
 return statuses[0],nil
}

// StartVMWithProgress uses the same engine as StartVM, reporting stages
// synchronously to callers without requiring a special CLI implementation.
func (e *Engine) StartVMWithProgress(ctx context.Context,name string,callback func(VMProgress))(*vm.Instance,error){
 if ctx==nil{return nil,errors.New("context must not be nil")}
 p,err:=e.GetProfile(name)
 if err!=nil{return nil,err}
 if err:=e.config.EnsureDirectories();err!=nil{return nil,err}
 return vm.StartWithProgress(ctx,&e.config,p,callback)
}

// StopVM destroys a disposable instance (by UUIDv7) and marks its history.
func(e *Engine)StopVM(ctx context.Context,id string)error{
 err:=vm.StopByID(ctx,&e.config,id)
 if err!=nil{return err}
 return e.closeShellsForVM(id)
}

// RunRequest selects the action to run and optionally overrides SSH settings.
// Either ManifestPath or Manifest must be supplied, never both.
type RunRequest struct {
	ManifestPath string
	Manifest     *Manifest
	// Host connects to an existing VM and bypasses Libvirt startup.
	Host string
	User string
	PrivateKey string
	KnownHosts string
	LogRoot string
	CommandTimeout time.Duration
	// Output receives streamed job lines; nil means no console output.
	// Regardless of Output, jobs always write persistent files.
	Output io.Writer
	// OnImportProgress receives structured events before each setup/job.
	OnImportProgress func(ImportProgress)
	// OnVMProgress receives VM startup progress when Run creates a guest.
	OnVMProgress func(VMProgress)
}

// RunResult records progress even if a job fails and Run returns an error.
type RunResult struct {
	SSHUser string
	VMID string
	MACAddress string
	Profile string
	Host string
	VMName string
	LogDirectory string
	CompletedJobs []string
	Imports []ImportResult
	StartedAt time.Time
	FinishedAt time.Time
}

// Run executes setup and jobs sequentially. The configured profile is not
// modified. Returning an error never implies that the log files are removed.
// Cleanup errors are joined with the original error.
func (e *Engine) Run(ctx context.Context, request RunRequest) (result *RunResult, runErr error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}
	if (request.Manifest == nil) == (request.ManifestPath == "") {
		return nil, errors.New("supply exactly one of Manifest or ManifestPath")
	}
	if request.CommandTimeout < 0 {
		return nil, errors.New("command timeout cannot be negative")
	}
	var m *manifest.Manifest
	if request.Manifest != nil {
		m = request.Manifest
		if err := m.Validate(); err != nil {
			return nil, err
		}
	} else {
		var err error
		m, err = manifest.ReadFile(request.ManifestPath)
		if err != nil {
			return nil, err
		}
	}
	result = &RunResult{
		Profile: m.Profile,
		StartedAt: time.Now(),
		CompletedJobs: []string{},
	}
	defer func() {
		result.FinishedAt = time.Now()
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := e.config.EnsureDirectories(); err != nil {
		return result, err
	}

	var p *profile.Profile
	if m.Profile != "" {
		var err error
		p, err = e.GetProfile(m.Profile)
		if err != nil {
			return result, fmt.Errorf("resolve profile %s: %w", m.Profile, err)
		}
	}
	if p == nil && request.Host == "" {
		return result, errors.New("no profile selected and no existing guest Host provided")
	}
	user, key, hosts := e.config.SSH.User, e.config.SSH.PrivateKey, e.config.SSH.KnownHosts
	host := request.Host
	if p != nil {
		if host == "" {
			host = p.SSH.Address
		}
		if p.SSH.User != "" {
			user = p.SSH.User
		}
		if p.SSH.PrivateKey != "" {
			key = p.SSH.PrivateKey
		}
		if p.SSH.KnownHosts != "" {
			hosts = p.SSH.KnownHosts
		}
	}
	if m.SSH.User != "" {
		user = m.SSH.User
	}
	if request.User != "" {
		user = request.User
	}
	if request.PrivateKey != "" {
		key = request.PrivateKey
	}
	if request.KnownHosts != "" {
		hosts = request.KnownHosts
	}
	logRoot := e.config.Storage.Logs
	if request.LogRoot != "" {
		logRoot = request.LogRoot
	}
	logRun, err := storage.NewRun(logRoot, m.Repository.Name, m.GroupName(), time.Now())
	if err != nil {
		return result, err
	}
	result.LogDirectory = logRun.Directory

	managedVM := host == ""
	if host == "" {
		// The VM overlay is private to this invocation. Other concurrent
		// runs using the same read-only backing image get their own overlay.
		// Provision the actual Action SSH account into the new guest.
		// The per-instance SSH keys are generated and managed by the VM runtime.
		configuredProfile := *p
		configuredProfile.SSH.User = user
		instance, err := vm.StartWithProgress(ctx, &e.config, &configuredProfile, request.OnVMProgress)
		if err != nil {
			return result, err
		}
		result.VMID = instance.ID
		result.VMName = instance.Name
		result.MACAddress = instance.MACAddress
		host = instance.Address
		// New disposable VMs use fresh, per-instance identities from Cloud-Init.
		// Existing hosts still use explicit or globally configured SSH keys.
		user, key, hosts = instance.SSHUser, instance.SSHPrivateKeyPath, instance.SSHKnownHostsPath
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			runErr = errors.Join(runErr, instance.Cleanup(cleanupCtx))
		}()
	}
	result.Host = host
	result.SSHUser = user
	if user == "" || key == "" || hosts == "" {
		return result, errors.New("SSH user, private key and known_hosts are required")
	}
	// Enforce ED25519 only for managed guests; preserve defaults for external SSH.
	var managedHostKeyAlgorithms []string
	if managedVM { managedHostKeyAlgorithms = []string{"ssh-ed25519"} }
	connectTimeout := time.Duration(e.config.SSH.ConnectTimeoutSeconds) * time.Second
	client, err := dialWithRetry(ctx, executor.DialConfig{
		Address: host,
		User: user,
		PrivateKeyPath: key,
		KnownHostsPath: hosts,
		Timeout: connectTimeout,
		HostKeyAlgorithms: managedHostKeyAlgorithms,
	}, connectTimeout)
	if err != nil {
		return result, err
	}
	defer client.Close()
	// The selected profile workspace applies to managed VMs and external SSH hosts.
	guestDestination := e.config.Guest.Destination
	if p != nil {
		guestDestination = p.GuestDestination(e.config.Guest.Destination)
	}
	if err:=ensureGuestWorkspace(ctx,client,guestDestination);err!=nil{
		return result,err
	}

	for _, item := range m.Imports {
		destination := guestDestination
		reporter := importReporter{out: request.Output,callback: request.OnImportProgress,
			source: item.Source,destination: destination}
		imported,importErr := importWithClient(ctx,client,item.Source,destination,reporter)
		if imported!=nil{result.Imports=append(result.Imports,*imported)}
		if importErr!=nil{
			reporter.send("failed","Falha na importacao: "+importErr.Error(),0,0,0)
			return result,fmt.Errorf("import %s to %s: %w",item.Source,destination,importErr)
		}
	}
	timeout := request.CommandTimeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	workspace := m.Workspace
	if workspace == "" {
		workspace = guestDestination
	}
	runner := &executor.Executor{
		Client: client,
		Workspace: workspace,
		Console: request.Output,
		CommandTimeout: timeout,
	}
	runJob := func(name string, commands []string, allowFailure bool) error {
		log, err := logRun.OpenJob(name)
		if err != nil {
			return err
		}
		jobErr := runner.Run(ctx, name, commands, allowFailure, log)
		closeErr := log.Close()
		if err := errors.Join(jobErr, closeErr); err != nil {
			return err
		}
		result.CompletedJobs = append(result.CompletedJobs, name)
		return nil
	}
	if len(m.Setup) > 0 {
		if err := runJob("setup", m.Setup, false); err != nil {
			return result, fmt.Errorf("setup: %w", err)
		}
	}
	for _, pipeline := range m.Pipelines {
		for _, job := range pipeline.Jobs {
			if err := runJob(job.Name, job.Commands, job.AllowFailure); err != nil {
				return result, fmt.Errorf("pipeline %s, job %s: %w", pipeline.Name, job.Name, err)
			}
		}
	}
	return result, nil
}

// dialWithRetry waits for sshd after a freshly created VM has obtained DHCP.
// This connection step uses the existing SSH package; the caller's context
// governs retries and the entire run, including cancellation.
func dialWithRetry(ctx context.Context, c executor.DialConfig, wait time.Duration) (*ssh.Client, error) {
	if wait <= 0 {
		wait = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("SSH unavailable: %w; last attempt: %v", err, last)
		}
		client, err := executor.Dial(c)
		if err == nil {
			return client, nil
		}
		last = err
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("SSH unavailable: %w; last attempt: %v", ctx.Err(), last)
		case <-timer.C:
		}
	}
}
