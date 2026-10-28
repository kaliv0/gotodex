package writer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// StopExtraction signals early stop (e.g. broken pipe on stdout-only).
type StopExtraction struct{}

func (StopExtraction) Error() string { return "stop extraction" }

// Writer writes extract output to a file and/or stdout (with optional TTY pager).
type Writer struct {
	stdout   io.Writer
	stderr   io.Writer
	file     *os.File
	pager    func(string)  // nil => default page(). TTY Close hook
	pagerBuf *bytes.Buffer // non-nil => TTY pager mode
	Path     string        // empty => no file
	mu       sync.Mutex
	ToStdout bool
}

// Options configures NewWith (prod + test injection of sinks / TTY / pager).
type Options struct {
	Stdout   io.Writer // nil => os.Stdout (ignored while TTY paging)
	Stderr   io.Writer // nil => os.Stderr
	Pager    func(string)
	Path     string
	ToStdout bool
	TTY      bool // when ToStdout: buffer then page on Close
}

// New builds a Writer. path empty and toStdout false is invalid for real runs.
// Callers set at least one sink. TTY from os.Stdout. Path set => open/truncate now.
func New(path string, toStdout bool) (*Writer, error) {
	return NewWith(Options{Path: path, ToStdout: toStdout, TTY: toStdout && stdoutIsTTY()})
}

// NewWith builds a Writer from Options (inject stdout/stderr/pager/TTY in tests).
// Path set => open/truncate immediately.
func NewWith(opts Options) (*Writer, error) {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	w := &Writer{
		Path:     opts.Path,
		ToStdout: opts.ToStdout,
		stdout:   stdout,
		stderr:   stderr,
		pager:    opts.Pager,
	}
	if opts.ToStdout && opts.TTY {
		w.pagerBuf = &bytes.Buffer{}
		w.stdout = w.pagerBuf
	}
	if opts.Path != "" {
		f, err := os.Create(opts.Path)
		if err != nil {
			return nil, err
		}
		w.file = f
	}
	return w, nil
}

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

// UsesPager reports whether stdout is buffered for a TTY pager.
func (w *Writer) UsesPager() bool {
	return w.pagerBuf != nil
}

func (w *Writer) WriteHeader(path string) error {
	return w.write(fmt.Sprintf("======================\n%s\n", path))
}

func (w *Writer) WriteEntry(lineNum int, line string) error {
	return w.write(fmt.Sprintf("\tline %d: %s", lineNum, line))
}

// Debug uses write (tee / StopExtraction like hits).
func (w *Writer) Debug(text string) error {
	return w.write(text)
}

// Error writes to stderr, or into the pager buffer when UsesPager.
func (w *Writer) Error(text string) error {
	if w.UsesPager() {
		return w.writeStdout(text)
	}
	_, err := io.WriteString(w.stderr, text)
	return err
}

// write sends to stdout and/or file. Stdout-only EPIPE -> StopExtraction.
// Tee EPIPE -> disableStdout then keep writing the file.
func (w *Writer) write(text string) error {
	if w.ToStdout {
		if err := w.writeStdout(text); err != nil {
			if !IsBrokenPipe(err) {
				return err
			}
			w.disableStdout()
			if w.Path == "" {
				return StopExtraction{}
			}
		}
	}
	return w.writeFile(text)
}

func (w *Writer) disableStdout() {
	w.ToStdout = false
	w.pagerBuf = nil
	w.stdout = os.Stdout
}

func (w *Writer) writeFile(text string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	// File opened in New/NewWith when Path set. Nil after Close or when Path empty.
	if w.file == nil {
		return nil
	}
	_, err := io.WriteString(w.file, text)
	return err
}

type flusher interface{ Flush() error }

func (w *Writer) writeStdout(text string) error {
	if _, err := io.WriteString(w.stdout, text); err != nil {
		return err
	}
	// Flush after write when not pager-buffered (os.File has no userspace buffer).
	if w.UsesPager() {
		return nil
	}
	if f, ok := w.stdout.(flusher); ok {
		return f.Flush()
	}
	return nil
}

func (w *Writer) Close() error {
	if w.UsesPager() {
		fn := page
		if w.pager != nil {
			fn = w.pager
		}
		fn(w.pagerBuf.String())
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// IsBrokenPipe reports whether err is EPIPE or a closed pipe (stdout consumer gone).
func IsBrokenPipe(err error) bool {
	return errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE)
}

func page(text string) {
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}
	if _, ok := os.LookupEnv("LESS"); !ok {
		_ = os.Setenv("LESS", "-RF")
	}
	// Split on whitespace so PAGER="less -R" works (no shell).
	cmd := pagerCmd(pager)
	cmd.Stdin = bytes.NewBufferString(text)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func pagerCmd(pager string) *exec.Cmd {
	fields := strings.Fields(pager)
	if len(fields) == 0 {
		return exec.Command("less")
	}
	return exec.Command(fields[0], fields[1:]...)
}
