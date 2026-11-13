package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/kaliv0/gotodex/internal/writer"
)

func TestUsageValidationErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		args []string
		need string
	}{
		{[]string{"-i", dir}, "-i/--ignore-default flag requires -t/--tokens"},
		{[]string{"-m", "20", dir}, "-m/--max-depth flag requires -r/--recursive"},
		{[]string{dir, "-s", "-o", "x"}, "shouldn't mix --stdout and --out or --tee flags"},
		{[]string{dir, "-s", "--tee"}, "shouldn't mix --stdout and --out or --tee flags"},
		{[]string{"-r", "-m", "0", dir}, "expected positive value"},
		{[]string{"-r", "-m", "-1", dir}, "expected positive value"},
	}
	for _, tt := range tests {
		requireExit2Contains(t, tt.args, tt.need)
	}
}

func TestMaxDepthRejectsNonIntViaExecute(t *testing.T) {
	dir := t.TempDir()
	code := finish(runRoot([]string{"-r", "-m", "xyz", dir}, io.Discard, &bytes.Buffer{}), io.Discard)
	if code != 2 {
		t.Fatalf("exit=%d want 2", code)
	}
}

// Smoke: flags reach the extractor (-t / -i / -o).
func TestArgvReachesExtractor(t *testing.T) {
	sample := materializeCLISample(t)
	out := filepath.Join(sample, "cli_out.txt")
	err := runRoot(
		[]string{filepath.Join(sample, "app.py"), "-o", out, "-t", "WARN", "-i"},
		io.Discard, &bytes.Buffer{},
	)
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	text := readFile(t, out)
	if !strings.Contains(text, "WARN") {
		t.Fatalf("missing WARN\n%s", text)
	}
	if strings.Contains(text, "TODO: main") {
		t.Fatalf("TODO must be ignored with -i\n%s", text)
	}
}

func writeTokenFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tokens.py")
	if err := os.WriteFile(path, []byte("# WARN: careful\n# REVISIT: later\n# TODO: skip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTokensStringSlice(t *testing.T) {
	src := writeTokenFixture(t)
	tests := []struct {
		name string
		args []string
	}{
		{"comma", []string{src, "-s", "-t", "WARN,REVISIT", "-i"}},
		{"repeat", []string{src, "-s", "-t", "WARN", "-t", "REVISIT", "-i"}},
		{"comma_trim", []string{src, "-s", "-t", "WARN, REVISIT", "-i"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			err := runRoot(tt.args, &stdout, &bytes.Buffer{})
			if err != nil {
				t.Fatalf("runRoot: %v", err)
			}
			text := stdout.String()
			for _, needle := range []string{"WARN: careful", "REVISIT: later"} {
				if !strings.Contains(text, needle) {
					t.Fatalf("missing %q\n%s", needle, text)
				}
			}
			if strings.Contains(text, "TODO: skip") {
				t.Fatalf("TODO must be ignored with -i\n%s", text)
			}
		})
	}
}

func TestExcludeCommaSeparated(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.py")
	skipA := filepath.Join(dir, "skip_a.py")
	skipB := filepath.Join(dir, "skip_b.py")
	for path, body := range map[string]string{
		keep:  "# TODO: keep\n",
		skipA: "# TODO: skip-a\n",
		skipB: "# TODO: skip-b\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var stdout bytes.Buffer
	err := runRoot(
		[]string{dir, "-s", "-r", "-x", "skip_a.py,skip_b.py"},
		&stdout, &bytes.Buffer{},
	)
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	text := stdout.String()
	if !strings.Contains(text, "TODO: keep") {
		t.Fatalf("missing keep\n%s", text)
	}
	for _, bad := range []string{"TODO: skip-a", "TODO: skip-b"} {
		if strings.Contains(text, bad) {
			t.Fatalf("excluded hit still present: %q\n%s", bad, text)
		}
	}
}

func TestSBrokenPipeViaCLIExits141(t *testing.T) {
	sample := materializeCLISample(t)
	tests := []struct {
		name string
		args []string
	}{
		{"write", []string{filepath.Join(sample, "app.py"), "-s"}},
		{"debug", []string{filepath.Join(sample, "app.py"), "-s", "-d"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := finish(runRoot(tt.args, brokenPipeWriter{}, &bytes.Buffer{}), io.Discard)
			if code != 141 {
				t.Fatalf("exit=%d want 141", code)
			}
		})
	}
}

func TestMissingPathExitsNonzero(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope")
	out := filepath.Join(dir, "x")
	err := runRoot([]string{missing, "-o", out}, io.Discard, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	var ee *exitError
	if errors.As(err, &ee) && ee.Code == 2 {
		t.Fatalf("usage exit unexpected: %v", err)
	}
	code := finish(runRoot([]string{missing, "-o", out}, io.Discard, &bytes.Buffer{}), io.Discard)
	if code != 1 {
		t.Fatalf("exit=%d want 1", code)
	}
}

func TestExecuteMapsUsageTo2(t *testing.T) {
	code := finish(runRoot([]string{"-i", t.TempDir()}, io.Discard, &bytes.Buffer{}), io.Discard)
	if code != 2 {
		t.Fatalf("exit=%d want 2", code)
	}
}

func TestExitCodeMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"validate", &exitError{Code: 2, Err: errors.New("x")}, 2},
		{"runtime", errors.New("nope not found"), 1},
		{"epipe", syscall.EPIPE, 141},
		{"closed_pipe", io.ErrClosedPipe, 141},
		{"stop", writer.ErrStopExtraction, 141},
		{"interrupt", errInterrupted, 130},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.err); got != tt.want {
				t.Fatalf("exitCode(%v)=%d want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestFinishWrites(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   int
		contain    []string
		notContain []string
	}{
		{
			"usage",
			&exitError{Code: 2, Err: errors.New("path required")},
			2,
			[]string{"gtx: error:", "path required"},
			nil,
		},
		{
			"runtime",
			errors.New("nope not found"),
			1,
			[]string{"nope not found"},
			[]string{"gtx: error:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := finish(tt.err, &stderr)
			if code != tt.wantCode {
				t.Fatalf("exit=%d want %d", code, tt.wantCode)
			}
			got := stderr.String()
			for _, s := range tt.contain {
				if !strings.Contains(got, s) {
					t.Fatalf("stderr=%q want contain %q", got, s)
				}
			}
			for _, s := range tt.notContain {
				if strings.Contains(got, s) {
					t.Fatalf("stderr=%q must not contain %q", got, s)
				}
			}
		})
	}
}

func TestFinishQuietCodesWriteNothing(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"ok", nil, 0},
		{"interrupt", errInterrupted, 130},
		{"stop", writer.ErrStopExtraction, 141},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := finish(tt.err, &stderr)
			if code != tt.want {
				t.Fatalf("exit=%d want %d", code, tt.want)
			}
			if got := stderr.String(); got != "" {
				t.Fatalf("stderr=%q want empty", got)
			}
		})
	}
}

func TestRunInterruptibleMapsCanceled(t *testing.T) {
	err := runInterruptible(func(_ context.Context) error {
		return context.Canceled
	})
	if !errors.Is(err, errInterrupted) {
		t.Fatalf("Canceled -> %v want errInterrupted", err)
	}
	if got := exitCode(err); got != 130 {
		t.Fatalf("exit=%d want 130", got)
	}
	if got := finish(err, io.Discard); got != 130 {
		t.Fatalf("finish=%d want 130", got)
	}
	other := errors.New("boom")
	err = runInterruptible(func(context.Context) error { return other })
	if !errors.Is(err, other) {
		t.Fatalf("other err -> %v", err)
	}
	if err := runInterruptible(func(context.Context) error { return nil }); err != nil {
		t.Fatalf("nil -> %v", err)
	}
}

func TestVersionShortFlag(t *testing.T) {
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"-v"})
	if err := root.Execute(); err != nil {
		t.Fatalf("gtx -v: %v\nout=%q", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "gtx version") {
		t.Fatalf("gtx -v want contain %q, got %q", "gtx version", got)
	}
	if !strings.Contains(got, Version) {
		t.Fatalf("gtx -v want contain %q, got %q", Version, got)
	}
}

func requireExit2Contains(t *testing.T, args []string, need string) {
	t.Helper()
	err := runRoot(args, io.Discard, &bytes.Buffer{})
	var ee *exitError
	if !errors.As(err, &ee) || ee.Code != 2 {
		t.Fatalf("args=%v: want exitError 2, got %v", args, err)
	}
	if !strings.Contains(ee.Err.Error(), need) {
		t.Fatalf("args=%v: got %q want contain %q", args, ee.Err.Error(), need)
	}
}

func materializeCLISample(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "testdata")
	dst := filepath.Join(t.TempDir(), "project")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy testdata: %v", err)
	}
	return dst
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type brokenPipeWriter struct{}

func (brokenPipeWriter) Write([]byte) (int, error) {
	return 0, syscall.EPIPE
}
