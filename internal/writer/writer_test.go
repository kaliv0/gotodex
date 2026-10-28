package writer_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"gotodex/internal/writer"
)

const (
	entryTODO1 = "\tline 1: # TODO: x\n"
	headerA    = "======================\na.py\n"
	payloadA   = headerA + entryTODO1
)

func mustNewWith(t *testing.T, opts writer.Options) *writer.Writer {
	t.Helper()
	w, err := writer.NewWith(opts)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func mustNew(t *testing.T, path string, toStdout bool) *writer.Writer {
	t.Helper()
	w, err := writer.New(path, toStdout)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestNonTTYUsesStdout(t *testing.T) {
	var out bytes.Buffer
	w := mustNewWith(t, writer.Options{
		ToStdout: true,
		TTY:      false,
		Stdout:   &out,
	})
	if w.UsesPager() {
		t.Fatal("non-TTY should not use pager")
	}
	if err := w.WriteEntry(1, "# TODO: x\n"); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != entryTODO1 {
		t.Fatalf("stdout=%q want %q", got, entryTODO1)
	}
}

func TestTTYBuffersAndPagesOnClose(t *testing.T) {
	var seen []string
	w := mustNewWith(t, writer.Options{
		ToStdout: true,
		TTY:      true,
		Pager:    func(text string) { seen = append(seen, text) },
	})
	if !w.UsesPager() {
		t.Fatal("TTY should use pager")
	}
	if err := w.WriteHeader("a.py"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteEntry(1, "# TODO: x\n"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatalf("pager called before Close: %v", seen)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != payloadA {
		t.Fatalf("pager=%q want %q", seen, payloadA)
	}
}

func TestTTYErrorGoesToPagerBuffer(t *testing.T) {
	var seen []string
	w := mustNewWith(t, writer.Options{
		ToStdout: true,
		TTY:      true,
		Pager:    func(text string) { seen = append(seen, text) },
	})
	if !w.UsesPager() {
		t.Fatal("TTY should use pager")
	}
	if err := w.Error("boom\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "boom\n" {
		t.Fatalf("pager=%q want [boom\\n]", seen)
	}
}

func TestNonTTYErrorGoesToStderr(t *testing.T) {
	var stderr bytes.Buffer
	w := mustNewWith(t, writer.Options{
		ToStdout: true,
		TTY:      false,
		Stdout:   io.Discard,
		Stderr:   &stderr,
	})
	if err := w.Error("boom\n"); err != nil {
		t.Fatal(err)
	}
	if got := stderr.String(); got != "boom\n" {
		t.Fatalf("stderr=%q want boom\\n", got)
	}
}

func TestTTYTeePagesAndWritesFile(t *testing.T) {
	var seen []string
	outPath := filepath.Join(t.TempDir(), "out.txt")
	w := mustNewWith(t, writer.Options{
		Path:     outPath,
		ToStdout: true,
		TTY:      true,
		Pager:    func(text string) { seen = append(seen, text) },
	})
	if err := w.WriteEntry(1, "# TODO: x\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != entryTODO1 {
		t.Fatalf("pager=%q want %q", seen, entryTODO1)
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != entryTODO1 {
		t.Fatalf("file=%q want %q", body, entryTODO1)
	}
}

func TestPageSetsLESSWhenUnset(t *testing.T) {
	unsetLESS(t)
	// Avoid spawning a real pager: `true` drains stdin and exits 0.
	t.Setenv("PAGER", "true")

	w := mustNewWith(t, writer.Options{
		ToStdout: true,
		TTY:      true,
	})
	if err := w.WriteEntry(1, "# TODO: x\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("LESS"); got != "-RF" {
		t.Fatalf("LESS=%q want -RF", got)
	}
}

// --- writer-level tee / EPIPE ---

func TestTeeWritesStdoutAndFile(t *testing.T) {
	var out bytes.Buffer
	outPath := filepath.Join(t.TempDir(), "out.txt")
	w := mustNewWith(t, writer.Options{
		Path:     outPath,
		ToStdout: true,
		TTY:      false,
		Stdout:   &out,
	})
	if err := w.WriteHeader("a.py"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteEntry(1, "# TODO: x\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != payloadA {
		t.Fatalf("stdout=%q want %q", got, payloadA)
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != payloadA {
		t.Fatalf("file=%q want %q", body, payloadA)
	}
}

func TestTeeSurvivesBrokenStdout(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.txt")
	w := mustNewWith(t, writer.Options{
		Path:     outPath,
		ToStdout: true,
		TTY:      false,
		Stdout:   brokenPipeWriter{},
	})
	if err := w.WriteHeader("a.py"); err != nil {
		t.Fatalf("tee+EPIPE should not fail write: %v", err)
	}
	if err := w.WriteEntry(1, "# TODO: x\n"); err != nil {
		t.Fatalf("tee+EPIPE should not fail write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != payloadA {
		t.Fatalf("file=%q want %q", body, payloadA)
	}
	if w.ToStdout {
		t.Fatal("ToStdout should be cleared after EPIPE")
	}
}

func TestStdoutOnlyStopsOnBrokenStdout(t *testing.T) {
	w := mustNewWith(t, writer.Options{
		ToStdout: true,
		TTY:      false,
		Stdout:   brokenPipeWriter{},
	})
	err := w.WriteEntry(1, "# TODO: x\n")
	if err != (writer.StopExtraction{}) {
		t.Fatalf("err=%v (%T) want StopExtraction", err, err)
	}
}

func TestFileOnlyWrite(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "TODO")
	w := mustNewWith(t, writer.Options{Path: outPath})
	if err := w.WriteHeader("a.py"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteEntry(2, "# FIXME: y\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	want := "======================\na.py\n\tline 2: # FIXME: y\n"
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != want {
		t.Fatalf("file=%q want %q", body, want)
	}
}

// Path set → open/truncate before Write*.
func TestOutPathTruncatedOnInit(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(outPath, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := mustNewWith(t, writer.Options{Path: outPath})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "" {
		t.Fatalf("file=%q want empty after NewWith+Close (no writes)", body)
	}
}

// Preserve existing LESS; page must not overwrite.
func TestPagePreservesLESSWhenSet(t *testing.T) {
	t.Setenv("LESS", "-S")
	t.Setenv("PAGER", "true")

	w := mustNewWith(t, writer.Options{
		ToStdout: true,
		TTY:      true,
	})
	if err := w.WriteEntry(1, "# TODO: x\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("LESS"); got != "-S" {
		t.Fatalf("LESS=%q want -S", got)
	}
}

// ErrClosedPipe also treated as broken pipe.
func TestTeeSurvivesErrClosedPipe(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.txt")
	w := mustNewWith(t, writer.Options{
		Path:     outPath,
		ToStdout: true,
		TTY:      false,
		Stdout:   errWriter{err: io.ErrClosedPipe},
	})
	if err := w.WriteEntry(1, "# TODO: x\n"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != entryTODO1 {
		t.Fatalf("file=%q want %q", body, entryTODO1)
	}
}

// EPIPE on Flush (after successful Write) still stops/tees like write EPIPE.
func TestTeeSurvivesBrokenFlush(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.txt")
	w := mustNewWith(t, writer.Options{
		Path:     outPath,
		ToStdout: true,
		TTY:      false,
		Stdout:   &flushPipeWriter{},
	})
	if err := w.WriteEntry(1, "# TODO: x\n"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if w.ToStdout {
		t.Fatal("ToStdout should be cleared after flush EPIPE")
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != entryTODO1 {
		t.Fatalf("file=%q want %q", body, entryTODO1)
	}
}

func TestStdoutOnlyStopsOnBrokenFlush(t *testing.T) {
	w := mustNewWith(t, writer.Options{
		ToStdout: true,
		TTY:      false,
		Stdout:   &flushPipeWriter{},
	})
	err := w.WriteEntry(1, "# TODO: x\n")
	if err != (writer.StopExtraction{}) {
		t.Fatalf("err=%v (%T) want StopExtraction", err, err)
	}
}

// PAGER may include args (split on whitespace, no shell).
func TestPageAcceptsPAGERWithArgs(t *testing.T) {
	t.Setenv("PAGER", "true -ignored")
	unsetLESS(t)

	w := mustNewWith(t, writer.Options{ToStdout: true, TTY: true})
	if err := w.WriteEntry(1, "# TODO: x\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// Tee + EPIPE: Debug disables stdout, keeps writing file.
func TestDebugTeeSurvivesBrokenStdout(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.txt")
	w := mustNewWith(t, writer.Options{
		Path:     outPath,
		ToStdout: true,
		TTY:      false,
		Stdout:   brokenPipeWriter{},
	})
	if err := w.Debug("dbg\n"); err != nil {
		t.Fatalf("tee+EPIPE Debug should not fail: %v", err)
	}
	if w.ToStdout {
		t.Fatal("ToStdout should be cleared after EPIPE")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "dbg\n" {
		t.Fatalf("file=%q want dbg\\n", body)
	}
}

// Stdout-only Debug + EPIPE → StopExtraction (same as hits).
func TestDebugStdoutOnlyStopsOnBrokenStdout(t *testing.T) {
	w := mustNewWith(t, writer.Options{
		ToStdout: true,
		TTY:      false,
		Stdout:   brokenPipeWriter{},
	})
	err := w.Debug("dbg\n")
	if err != (writer.StopExtraction{}) {
		t.Fatalf("err=%v (%T) want StopExtraction", err, err)
	}
}

func TestNewMatchesNewWithTTYDetection(t *testing.T) {
	tty := false
	if fi, err := os.Stdout.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		tty = true
	}
	w := mustNew(t, "", true)
	if w.UsesPager() != tty {
		t.Fatalf("New UsesPager=%v want %v", w.UsesPager(), tty)
	}
	w2 := mustNewWith(t, writer.Options{ToStdout: true, TTY: tty})
	if w2.UsesPager() != tty {
		t.Fatalf("NewWith UsesPager=%v want %v", w2.UsesPager(), tty)
	}
	if mustNew(t, "", false).UsesPager() {
		t.Fatal("New(toStdout=false) must not use pager")
	}
}

func unsetLESS(t *testing.T) {
	t.Helper()
	orig, had := os.LookupEnv("LESS")
	_ = os.Unsetenv("LESS")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("LESS", orig)
		} else {
			_ = os.Unsetenv("LESS")
		}
	})
}

type brokenPipeWriter struct{}

func (brokenPipeWriter) Write([]byte) (int, error) {
	return 0, syscall.EPIPE
}

type errWriter struct{ err error }

func (e errWriter) Write([]byte) (int, error) {
	return 0, e.err
}

// flushPipeWriter succeeds Write then fails Flush with EPIPE.
type flushPipeWriter struct{}

func (f *flushPipeWriter) Write(p []byte) (int, error) { return len(p), nil }
func (f *flushPipeWriter) Flush() error                { return syscall.EPIPE }

// Unix-only: EPIPE lives in writer.go; no GOOS-split brokenpipe_*.go stubs.
func TestNoBrokenPipeSplitFiles(t *testing.T) {
	matches, err := filepath.Glob("brokenpipe_*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("want no brokenpipe_*.go, got %v", matches)
	}
}
