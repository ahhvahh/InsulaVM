// Package storage records the complete request/response history of SSH jobs.
// A run owns a unique timestamped directory, while each job owns one log file.
package storage

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Run represents a unique execution directory. Its files are retained after
// execution; cleaning them up is a separate retention-policy concern.
type Run struct {
	Directory string
}

// ValidateName ensures untrusted YAML values cannot escape the log directory.
func ValidateName(name string) error {
	if name == "" || name == "." || name == ".." || len(name) > 128 {
		return fmt.Errorf("invalid name %q", name)
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return fmt.Errorf("name %q must contain only ASCII letters, digits, '.', '_' or '-'", name)
		}
	}
	if name[0] == '.' {
		return fmt.Errorf("name %q cannot begin with '.'", name)
	}
	return nil
}

// NewRun creates <root>/<repository>/<action>/<YYYYMMDD_HHMMSS>.
// When a second run starts in the same second, a numeric suffix avoids
// overwriting the first run.
func NewRun(root, repository, action string, when time.Time) (*Run, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("log root is required")
	}
	for _, name := range []string{repository, action} {
		if err := ValidateName(name); err != nil {
			return nil, err
		}
	}
	parent := filepath.Join(root, repository, action)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, fmt.Errorf("create log directories: %w", err)
	}
	stamp := when.Format("20060102_150405")
	for attempt := 0; attempt < 1000; attempt++ {
		name := stamp
		if attempt != 0 {
			name = fmt.Sprintf("%s_%03d", stamp, attempt)
		}
		dir := filepath.Join(parent, name)
		err := os.Mkdir(dir, 0700)
		if err == nil {
			return &Run{Directory: dir}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create run directory: %w", err)
		}
	}
	return nil, errors.New("too many runs started in the same second")
}

// JobLog is a line-oriented, concurrency-safe log with a buffered writer.
// Flush happens on every line so the file can be tailed during execution.
type JobLog struct {
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
	closed bool
}

// OpenJob exclusively creates <job>.log; existing jobs are never truncated.
func (r *Run) OpenJob(job string) (*JobLog, error) {
	if err := ValidateName(job); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(r.Directory, job+".log"),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open job %q log: %w", job, err)
	}
	return &JobLog{file: file, writer: bufio.NewWriterSize(file, 64*1024)}, nil
}

// Request records exactly the script sent to the guest, with >> markers.
func (l *JobLog) Request(script string) error {
	return l.writeLines(">> ", script)
}

// Response records one line from either stdout or stderr, with << markers.
func (l *JobLog) Response(line string) error {
	return l.writeLines("<< ", line)
}

func (l *JobLog) writeLines(marker, value string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return os.ErrClosed
	}
	value = strings.TrimSuffix(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	for _, line := range strings.Split(value, "\n") {
		if _, err := l.writer.WriteString(marker + line + "\n"); err != nil {
			return err
		}
	}
	return l.writer.Flush()
}

// Flush synchronizes buffered log bytes after a completed command.
func (l *JobLog) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return os.ErrClosed
	}
	if err := l.writer.Flush(); err != nil {
		return err
	}
	return l.file.Sync()
}

// Close flushes and syncs the final log before closing the descriptor.
func (l *JobLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return errors.Join(l.writer.Flush(), l.file.Sync(), l.file.Close())
}
