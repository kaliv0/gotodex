package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"gotodex/internal/writer"
)

// appOut is the expected default scan of sample/app.py.
const appOut = "======================\napp.py\n\tline 1: # TODO: main\n\tline 2: # FIXME: bug\n"

func TestMissingDependentFlagErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		args []string
		need string // substring in error
	}{
		{[]string{"-i", dir}, "-i/--ignore-default flag requires -t/--tokens"},
		{[]string{"-m", "20", dir}, "-m/--max-depth flag requires -r/--recursive"},
	}
	for _, tt := range tests {
		err := runRoot(tt.args, io.Discard, &bytes.Buffer{})
		var ee *exitError
		if !errors.As(err, &ee) || ee.Code != 2 {
			t.Fatalf("args=%v: want exitError 2, got %v", tt.args, err)
		}
		if !strings.Contains(ee.Err.Error(), tt.need) {
			t.Fatalf("args=%v: got %q want contain %q", tt.args, ee.Err.Error(), tt.need)
		}
	}
}

func TestStdoutMixErrors(t *testing.T) {
	dir := t.TempDir()
	for _, extra := range [][]string{
		{"-s", "-o", "x"},
		{"-s", "--tee"},
	} {
		args := append([]string{dir}, extra...)
		err := runRoot(args, io.Discard, &bytes.Buffer{})
		var ee *exitError
		if !errors.As(err, &ee) || ee.Code != 2 {
			t.Fatalf("args=%v: want exitError 2, got %v", args, err)
		}
		if ee.Err.Error() != "shouldn't mix --stdout and --out or --tee flags" {
			t.Fatalf("got %q", ee.Err.Error())
		}
	}
}

func TestMaxDepthRejectsInvalidValues(t *testing.T) {
	dir := t.TempDir()
	for _, value := range []string{"0", "-1"} {
		err := runRoot([]string{"-r", "-m", value, dir}, io.Discard, &bytes.Buffer{})
		var ee *exitError
		if !errors.As(err, &ee) || ee.Code != 2 {
			t.Fatalf("m=%s: want exitError 2, got %v", value, err)
		}
		if !strings.Contains(ee.Err.Error(), "expected positive value") {
			t.Fatalf("m=%s: %v", value, ee.Err)
		}
	}
}

// Non-int -m still exits 2 via finish (Cobra parse error).
func TestMaxDepthRejectsNonIntViaExecute(t *testing.T) {
	dir := t.TempDir()
	code := finish(runRoot([]string{"-r", "-m", "xyz", dir}, io.Discard, &bytes.Buffer{}))
	if code != 2 {
		t.Fatalf("exit=%d want 2", code)
	}
}

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

func TestRMLimitsDepthViaCLI(t *testing.T) {
	sample := materializeCLISample(t)
	out := filepath.Join(sample, "cli_depth_out.txt")
	err := runRoot([]string{sample, "-o", out, "-r", "-m", "1"}, io.Discard, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	text := readFile(t, out)
	if !strings.Contains(text, "TODO: deep") {
		t.Fatalf("missing TODO: deep\n%s", text)
	}
	if strings.Contains(text, "TODO: deeper") {
		t.Fatalf("TODO: deeper must be beyond -m 1\n%s", text)
	}
}

func TestSViaCLI(t *testing.T) {
	sample := materializeCLISample(t)
	var stdout bytes.Buffer
	err := runRoot(
		[]string{filepath.Join(sample, "app.py"), "-s"},
		&stdout, &bytes.Buffer{},
	)
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if got := stdout.String(); got != appOut {
		t.Fatalf("stdout=%q want %q", got, appOut)
	}
}

func TestTeeViaCLI(t *testing.T) {
	sample := materializeCLISample(t)
	out := filepath.Join(sample, "cli_tee_out.txt")
	var stdout bytes.Buffer
	err := runRoot(
		[]string{filepath.Join(sample, "app.py"), "-o", out, "--tee"},
		&stdout, &bytes.Buffer{},
	)
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if got := readFile(t, out); got != appOut {
		t.Fatalf("file=%q want %q", got, appOut)
	}
	if got := stdout.String(); got != appOut {
		t.Fatalf("stdout=%q want %q", got, appOut)
	}
}

// -d debug writes stdout outside Writer.write → EPIPE reaches Execute → 141.
func TestSBrokenPipeViaDebugExits141(t *testing.T) {
	sample := materializeCLISample(t)
	code := finish(runRoot(
		[]string{filepath.Join(sample, "app.py"), "-s", "-d"},
		brokenPipeWriter{},
		&bytes.Buffer{},
	))
	if code != 141 {
		t.Fatalf("exit=%d want 141", code)
	}
}

// Stdout-only write EPIPE → StopExtraction → exit 141.
func TestSBrokenPipeWriteExits141(t *testing.T) {
	sample := materializeCLISample(t)
	code := finish(runRoot(
		[]string{filepath.Join(sample, "app.py"), "-s"},
		brokenPipeWriter{},
		&bytes.Buffer{},
	))
	if code != 141 {
		t.Fatalf("exit=%d want 141 (StopExtraction must reach finish)", code)
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
	code := finish(runRoot([]string{missing, "-o", out}, io.Discard, &bytes.Buffer{}))
	if code != 1 {
		t.Fatalf("exit=%d want 1", code)
	}
}

func TestExecuteMapsUsageTo2(t *testing.T) {
	code := finish(runRoot([]string{"-i", t.TempDir()}, io.Discard, &bytes.Buffer{}))
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
		{"stop", writer.StopExtraction{}, 141},
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

// runInterruptible: context.Canceled → errInterrupted → exit 130.
func TestRunInterruptibleMapsCanceled(t *testing.T) {
	err := runInterruptible(func(ctx context.Context) error {
		return context.Canceled
	})
	if !errors.Is(err, errInterrupted) {
		t.Fatalf("Canceled → %v want errInterrupted", err)
	}
	if got := exitCode(err); got != 130 {
		t.Fatalf("exit=%d want 130", got)
	}
	if got := finish(err); got != 130 {
		t.Fatalf("finish=%d want 130", got)
	}
	other := errors.New("boom")
	err = runInterruptible(func(context.Context) error { return other })
	if !errors.Is(err, other) {
		t.Fatalf("other err → %v", err)
	}
	if err := runInterruptible(func(context.Context) error { return nil }); err != nil {
		t.Fatalf("nil → %v", err)
	}
}

func TestHelpContainsTrashBanner(t *testing.T) {
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	help := out.String()
	// Distinctive banner lines — do not require byte-identical art.
	for _, needle := range []string{"GTX", "Peak trash sniffer"} {
		if !strings.Contains(help, needle) {
			t.Errorf("help missing %q (want banner art / long description)\nhelp:\n%s", needle, help)
		}
	}
}

// cmd/gtx must exist and call cli.Execute.
func TestCmdGTXMainExists(t *testing.T) {
	mainPath := filepath.Join(repoRoot(t), "cmd", "gtx", "main.go")
	b, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("cmd/gtx/main.go required: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, "cli.Execute") {
		t.Fatal("cmd/gtx/main.go must call cli.Execute")
	}
}

// cmd/todex and cmd/tdx must not exist.
func TestCmdTodexAndTDXGone(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{"todex", "tdx"} {
		path := filepath.Join(root, "cmd", name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("cmd/%s must not exist (got err=%v)", name, err)
		}
	}
}

func TestBuildGTXVersion(t *testing.T) {
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "gtx")
	build := exec.Command("go", "build", "-o", bin, "./cmd/gtx")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/gtx: %v\n%s", err, out)
	}
	ver := exec.Command(bin, "--version")
	out, err := ver.CombinedOutput()
	if err != nil {
		t.Fatalf("gtx --version: %v\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "gtx version") {
		t.Fatalf("gtx --version want %q, got %q", "gtx version", got)
	}
}

// README must mention key short flags.
func TestREADMEMentionsKeyFlags(t *testing.T) {
	path := filepath.Join(repoRoot(t), "README.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("README.md required: %v", err)
	}
	text := string(b)
	for _, flag := range []string{"-t", "-r", "-s", "-o"} {
		if !strings.Contains(text, flag) {
			t.Errorf("README.md missing flag %q", flag)
		}
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

func repoRoot(t *testing.T) string {
	t.Helper()
	// internal/cli → repo root (same as materializeCLISample).
	return filepath.Clean(filepath.Join("..", ".."))
}
