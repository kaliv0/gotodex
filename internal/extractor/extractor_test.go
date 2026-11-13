package extractor

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const appOut = "======================\napp.py\n\tline 1: # TODO: main\n\tline 2: # FIXME: bug\n"

func TestDefaultScan(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, filepath.Join(sample, "app.py"), out, Options{})
	mustContain(t, text, "TODO: main", "FIXME: bug")
	mustNotContain(t, text, "WARN: careful")
}

func TestTAddsTokenAndKeepsDefaults(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, filepath.Join(sample, "app.py"), out, Options{
		Tokens: []string{"WARN"},
	})
	mustContain(t, text, "WARN: careful", "TODO: main")
}

func TestIWithTUsesOnlyCustomTokens(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, filepath.Join(sample, "app.py"), out, Options{
		Tokens:        []string{"WARN"},
		IgnoreDefault: true,
	})
	mustContain(t, text, "WARN: careful")
	mustNotContain(t, text, "TODO: main")
}

func TestCustomTokensSmoke(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, filepath.Join(sample, "custom_tokens.py"), out, Options{
		Tokens:        []string{"NOTE", "!!!URGENT!!!", "@FIXME", "中文标记"},
		IgnoreDefault: true,
	})
	mustContain(t, text, "plain note", "!!!URGENT!!!", "at-prefixed fixme", "non-ascii")
}

func TestODirWritesTodoInside(t *testing.T) {
	sample := materializeSample(t)
	reports := filepath.Join(sample, "reports")
	if err := os.Mkdir(reports, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, mustNew(t, Options{
		Path: filepath.Join(sample, "app.py"),
		Out:  reports,
	}))
	got := readFile(t, filepath.Join(reports, "TODO"))
	if got != appOut {
		t.Fatalf("reports/TODO:\n got %q\nwant %q", got, appOut)
	}
}

func TestOmittedOutDefaultsToTodo(t *testing.T) {
	sample := materializeSample(t)
	t.Chdir(sample)
	mustRun(t, mustNew(t, Options{Path: "app.py"}))
	got := readFile(t, filepath.Join(sample, "TODO"))
	if got != appOut {
		t.Fatalf("TODO:\n got %q\nwant %q", got, appOut)
	}
}

func TestSWritesToStdoutOnly(t *testing.T) {
	sample := materializeSample(t)
	var buf bytes.Buffer
	ex := mustNew(t, Options{
		Path:    filepath.Join(sample, "app.py"),
		Stdout:  true,
		StdoutW: &buf,
	})
	mustRun(t, ex)
	if got := buf.String(); got != appOut {
		t.Fatalf("stdout:\n got %q\nwant %q", got, appOut)
	}
	if _, err := os.Stat(filepath.Join(sample, "TODO")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("TODO file must not exist with -s, err=%v", err)
	}
}

func TestTeeWritesStdoutAndFile(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	var buf bytes.Buffer
	ex := mustNew(t, Options{
		Path:    filepath.Join(sample, "app.py"),
		Out:     out,
		Tee:     true,
		StdoutW: &buf,
	})
	mustRun(t, ex)
	if got := readFile(t, out); got != appOut {
		t.Fatalf("file:\n got %q\nwant %q", got, appOut)
	}
	if got := buf.String(); got != appOut {
		t.Fatalf("stdout:\n got %q\nwant %q", got, appOut)
	}
}

func TestTeeWithoutOutUsesTodo(t *testing.T) {
	sample := materializeSample(t)
	t.Chdir(sample)
	var buf bytes.Buffer
	ex := mustNew(t, Options{Path: "app.py", Tee: true, StdoutW: &buf})
	mustRun(t, ex)
	if got := readFile(t, filepath.Join(sample, "TODO")); got != appOut {
		t.Fatalf("TODO:\n got %q\nwant %q", got, appOut)
	}
	if got := buf.String(); got != appOut {
		t.Fatalf("stdout:\n got %q\nwant %q", got, appOut)
	}
}

func TestMissingPathRaises(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	ex := mustNew(t, Options{
		Path: filepath.Join(dir, "missing"),
		Out:  out,
	})
	err := ex.Run(context.Background())
	if err == nil {
		t.Fatal("want error for missing path")
	}
	mustContain(t, err.Error(), "not found")
	if _, err := os.Stat(out); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("out must not be created on missing PATH, err=%v", err)
	}
}

// extra vs Python: wrap processFile os.Open failures with %w
func TestOpenFailureWrapped(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "locked.py")
	mustWrite(t, path, "# TODO: x\n")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if f, err := os.Open(path); err == nil {
		_ = f.Close()
		t.Skip("open succeeded despite mode 0")
	}

	var buf bytes.Buffer
	ex := mustNew(t, Options{Path: path, Stdout: true, StdoutW: &buf})
	err := ex.Run(context.Background())
	assertWrappedFS(t, err, "open", path, os.ErrPermission)
}

// extra vs Python: wrap processDir os.ReadDir failures with %w
func TestReadDirFailureWrapped(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	locked := filepath.Join(dir, "noperm")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(locked, "x.py"), "# TODO: x\n")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("readdir succeeded despite mode 0")
	}

	var buf bytes.Buffer
	ex := mustNew(t, Options{Path: locked, Stdout: true, StdoutW: &buf})
	err := ex.Run(context.Background())
	assertWrappedFS(t, err, "readdir", locked, os.ErrPermission)
}

func TestMultipleHitsOneHeaderAndLineNumbers(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "multi.py")
	out := filepath.Join(dir, "out.txt")
	mustWrite(t, src, "line1\n# TODO{2} first\nbody\n# FIXME: second\n")
	text := runToOut(t, src, out, Options{})
	if strings.Count(text, "======================") != 1 {
		t.Fatalf("want one header, got:\n%s", text)
	}
	mustContain(t, text, "line 2:", "line 4:", "TODO", "FIXME", "body")
}

func TestZeroTokenFileEmptyOut(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "newline", content: "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, tc.name+".py")
			out := filepath.Join(dir, "out.txt")
			mustWrite(t, src, tc.content)
			mustRun(t, mustNew(t, Options{Path: src, Out: out}))
			if got := readFile(t, out); got != "" {
				t.Fatalf("out=%q want empty", got)
			}
		})
	}
}

func TestOEqualsScannedFileTruncates(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "f.py")
	mustWrite(t, src, "# TODO: will be truncated\n")
	ex := mustNew(t, Options{Path: src, Out: src})
	done := make(chan error, 1)
	go func() { done <- ex.Run(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run hung; want eager truncate so Out==Path finishes empty")
	}
	if got := readFile(t, src); got != "" {
		t.Fatalf("src=%q want empty", got)
	}
}

func TestZeroHitScanTruncatesStaleOut(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.py")
	out := filepath.Join(dir, "out.txt")
	mustWrite(t, src, "print(1)\n")
	mustWrite(t, out, "stale from previous run\n")
	mustRun(t, mustNew(t, Options{Path: src, Out: out}))
	if got := readFile(t, out); got != "" {
		t.Fatalf("out=%q want empty (stale truncated)", got)
	}
}

func TestRFindsNested(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{Recursive: true})
	mustContain(t, text, "TODO: deep", "TODO: deeper")
}

func TestMaxDepthZeroUsesDefault120(t *testing.T) {
	if DefaultMaxDepth != 120 {
		t.Fatalf("DefaultMaxDepth=%d want 120", DefaultMaxDepth)
	}
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{Recursive: true, MaxDepth: 0})
	mustContain(t, text, "TODO: deeper")
}

// extra vs Python: library MaxDepth <= 0 => DefaultMaxDepth (normalized in New)
func TestNewNormalizesNonPositiveMaxDepth(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []int{0, -1} {
		ex := mustNew(t, Options{Path: dir, MaxDepth: d})
		if ex.opts.MaxDepth != DefaultMaxDepth {
			t.Fatalf("after New(MaxDepth=%d): opts.MaxDepth=%d want %d",
				d, ex.opts.MaxDepth, DefaultMaxDepth)
		}
	}
}

// extra vs Python: library MaxDepth -1 => DefaultMaxDepth (CLI still rejects)
func TestMaxDepthNegativeUsesDefault120(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{Recursive: true, MaxDepth: -1})
	mustContain(t, text, "TODO: deeper")
}

func TestRMLimitsDepth(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{
		Recursive: true,
		MaxDepth:  1,
	})
	mustContain(t, text, "TODO: deep")
	mustNotContain(t, text, "TODO: deeper")
}

func TestWithoutRSkipsNested(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{})
	mustNotContain(t, text, "TODO: deep", "TODO: deeper")
	mustContain(t, text, "TODO: main")
}

func TestDirWalkOrderFilesThenDirsByName(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	for _, name := range []string{"c.py", "a.py", "b.py"} {
		mustWrite(t, filepath.Join(dir, name), "# TODO: "+name+"\n")
	}
	mustWrite(t, filepath.Join(dir, "d", "z.py"), "# TODO: nested\n")
	text := runToOut(t, dir, out, Options{Recursive: true})
	markers := []string{"TODO: a.py", "TODO: b.py", "TODO: c.py", "TODO: nested"}
	prev := -1
	for _, m := range markers {
		i := strings.Index(text, m)
		if i < 0 {
			t.Fatalf("missing %q\n%s", m, text)
		}
		if i <= prev {
			t.Fatalf("order: %q at %d, prior end %d\n%s", m, i, prev, text)
		}
		prev = i
	}
}

func TestSymlinkPathRaises(t *testing.T) {
	cases := []struct {
		name      string
		targetDir bool
	}{
		{name: "file", targetDir: false},
		{name: "dir", targetDir: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			var target, link string
			if tc.targetDir {
				target = filepath.Join(dir, "dir")
				if err := os.Mkdir(target, 0o755); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, filepath.Join(target, "x.py"), "# TODO: x\n")
				link = filepath.Join(dir, "linkdir")
			} else {
				target = filepath.Join(dir, "file.py")
				mustWrite(t, target, "# TODO: x\n")
				link = filepath.Join(dir, "link.py")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatalf("symlink: %v", err)
			}
			out := filepath.Join(dir, "out.txt")
			ex := mustNew(t, Options{Path: link, Out: out})
			err := ex.Run(context.Background())
			if err == nil {
				t.Fatal("want error for symlink PATH")
			}
			mustContain(t, err.Error(), "is a symlink")
			if _, err := os.Stat(out); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("out must not be created when PATH is a symlink, err=%v", err)
			}
		})
	}
}

func TestExtraSymlinkDirNotFollowed(t *testing.T) {
	scan := t.TempDir()
	outside := t.TempDir()
	out := filepath.Join(scan, "out.txt")
	mustWrite(t, filepath.Join(outside, "via_link.py"), "# TODO: through-symlink\n")
	mustWrite(t, filepath.Join(scan, "top.py"), "# TODO: top\n")
	if err := os.Symlink(outside, filepath.Join(scan, "linkdir")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	text := runToOut(t, scan, out, Options{Recursive: true})
	mustContain(t, text, "TODO: top")
	mustNotContain(t, text, "TODO: through-symlink")
}

func TestExtraSymlinkFileNotScanned(t *testing.T) {
	scan := t.TempDir()
	outside := t.TempDir()
	out := filepath.Join(scan, "out.txt")
	mustWrite(t, filepath.Join(outside, "target.py"), "# TODO: via-file-link\n")
	mustWrite(t, filepath.Join(scan, "top.py"), "# TODO: top\n")
	if err := os.Symlink(filepath.Join(outside, "target.py"), filepath.Join(scan, "link.py")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	text := runToOut(t, scan, out, Options{})
	mustContain(t, text, "TODO: top")
	mustNotContain(t, text, "TODO: via-file-link")
}

func TestFWritesAbsolutePath(t *testing.T) {
	sample := materializeSample(t)
	target := filepath.Join(sample, "app.py")
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, target, out, Options{FullPath: true})
	abs, err := filepath.Abs(target)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, text, abs)
}

func TestDoesNotRescanOutputFile(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "TODO")
	mustWrite(t, out, "======================\nstale.py\n\tline 1: # TODO: from previous run\n")
	ex := mustNew(t, Options{
		Path:      sample,
		Out:       out,
		Recursive: true,
	})
	mustRunWithin(t, ex, 2*time.Second)
	text := readFile(t, out)
	mustNotContain(t, text, "TODO: from previous run")
	mustContain(t, text, "TODO: main")
}

func TestSkipsUndecodableFileAndContinues(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "good.py"), "# TODO: keep me\n")
	if err := os.WriteFile(filepath.Join(dir, "bad.bin"), []byte("\xff\xfe TODO: binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.txt")
	var stderr bytes.Buffer
	ex := mustNew(t, Options{
		Path:    dir,
		Out:     out,
		Debug:   true,
		StderrW: &stderr,
	})
	mustRun(t, ex)
	mustContain(t, readFile(t, out), "TODO: keep me")
	mustContain(t, stderr.String(), "skipping", "bad.bin")
}

func TestDebugEmitsProcessingTrail(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "good.py"), "# TODO: keep me\n")
	out := filepath.Join(dir, "out.txt")
	var stdout, stderr bytes.Buffer
	ex := mustNew(t, Options{
		Path:    dir,
		Out:     out,
		Debug:   true,
		StdoutW: &stdout,
		StderrW: &stderr,
	})
	mustRun(t, ex)
	mustContain(t, readFile(t, out), "processing:\n", "\tgood.py\n")
	if stdout.Len() != 0 {
		t.Fatalf("file-only -d: stdout empty, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("no skip expected; stderr=%q", stderr.String())
	}
}

func TestDoesNotRescanOutputFileRelativeOut(t *testing.T) {
	sample := materializeSample(t)
	t.Chdir(sample)
	mustWrite(t, filepath.Join(sample, "TODO"),
		"======================\nstale.py\n\tline 1: # TODO: from previous run\n")
	ex := mustNew(t, Options{
		Path:      ".",
		Out:       "TODO",
		Recursive: true,
	})
	mustRunWithin(t, ex, 2*time.Second)
	text := readFile(t, filepath.Join(sample, "TODO"))
	mustNotContain(t, text, "TODO: from previous run")
	mustContain(t, text, "TODO: main")
}

func TestSkipsUndecodableMidFileKeepsPriorHits(t *testing.T) {
	dir := t.TempDir()
	body := "# TODO: before\n" + string([]byte{0xff, 0xfe}) + " TODO: after\n"
	mustWrite(t, filepath.Join(dir, "mixed.py"), body)
	out := filepath.Join(dir, "out.txt")
	var stderr bytes.Buffer
	ex := mustNew(t, Options{
		Path:    filepath.Join(dir, "mixed.py"),
		Out:     out,
		Debug:   true,
		StderrW: &stderr,
	})
	mustRun(t, ex)
	text := readFile(t, out)
	mustContain(t, text, "TODO: before")
	mustNotContain(t, text, "TODO: after")
	mustContain(t, stderr.String(), "skipping")
}

func TestSkipsUndecodableSilentWithoutDebug(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "good.py"), "# TODO: keep me\n")
	if err := os.WriteFile(filepath.Join(dir, "bad.bin"), []byte("\xff\xfe TODO: binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.txt")
	var stderr bytes.Buffer
	ex := mustNew(t, Options{Path: dir, Out: out, StderrW: &stderr})
	mustRun(t, ex)
	mustContain(t, readFile(t, out), "TODO: keep me")
	if stderr.Len() != 0 {
		t.Fatalf("want silent skip without Debug, stderr=%q", stderr.String())
	}
}

func TestRunCanceledMidWalk(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	for _, name := range []string{"a.py", "b.py", "c.py"} {
		mustWrite(t, filepath.Join(dir, name), "# TODO: "+name+"\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	var processed atomic.Int32
	ex := mustNew(t, Options{Path: dir, Out: out})
	ex.afterFile = func(string) {
		if processed.Add(1) == 1 {
			cancel()
		}
	}

	err := ex.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run err=%v want context.Canceled", err)
	}
	n := processed.Load()
	if n != 1 {
		t.Fatalf("processed %d files, want 1 (stop before b.py/c.py)", n)
	}
}

func TestRunPreCanceledCtx(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	mustWrite(t, filepath.Join(dir, "a.py"), "# TODO: a\n")
	mustWrite(t, filepath.Join(dir, "b.py"), "# TODO: b\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var processed atomic.Int32
	ex := mustNew(t, Options{Path: dir, Out: out})
	ex.afterFile = func(string) { processed.Add(1) }

	err := ex.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run err=%v want context.Canceled", err)
	}
	if n := processed.Load(); n != 0 {
		t.Fatalf("processed %d files, want 0", n)
	}
}

func TestIsExcludedSkipsOutPath(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "TODO")
	ex, err := New(Options{Path: dir, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if err := ex.setOut(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ex.out.Close() }()

	if !ex.isExcluded(out, false) {
		t.Fatal("want out path excluded (absolute compare)")
	}
	t.Chdir(dir)
	if !ex.isExcluded("TODO", false) {
		t.Fatal("want relative out path excluded via abs compare")
	}
}

func TestIsExcludedStdoutHasNoOutSkip(t *testing.T) {
	dir := t.TempDir()
	todo := filepath.Join(dir, "TODO")
	ex, err := New(Options{Path: dir, Out: todo, Stdout: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ex.setOut(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ex.out.Close() }()

	if ex.isExcluded(todo, false) {
		t.Fatal("stdout-only must not treat Opts.Out as a skip path")
	}
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("chmod modes ineffective as root")
	}
}

// assertWrappedFS checks %w wrap: errors.Is target, op+path in message, not bare *os.PathError.
func assertWrappedFS(t *testing.T, err error, op, path string, target error) {
	t.Helper()
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, target) {
		t.Fatalf("errors.Is(%v, %v)=false", err, target)
	}
	msg := err.Error()
	if !strings.Contains(msg, op) {
		t.Fatalf("want %q in %q", op, msg)
	}
	if !strings.Contains(msg, path) {
		t.Fatalf("want path %q in %q", path, msg)
	}
	if _, ok := err.(*os.PathError); ok {
		t.Fatalf("want wrapped error, got bare *os.PathError: %v", err)
	}
}

func mustContain(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Fatalf("missing %q\n%s", w, got)
		}
	}
}

func mustNotContain(t *testing.T, got string, forbid ...string) {
	t.Helper()
	for _, w := range forbid {
		if strings.Contains(got, w) {
			t.Fatalf("unexpected %q\n%s", w, got)
		}
	}
}

func materializeSample(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "testdata")
	dst := filepath.Join(t.TempDir(), "project")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy testdata: %v", err)
	}
	mustWrite(t, filepath.Join(dst, "skip_me.py"), "# TODO: skip\n")
	mustWrite(t, filepath.Join(dst, "vendor", "lib.py"), "# TODO: vendored\n")
	mustWrite(t, filepath.Join(dst, ".venv", "x.py"), "# TODO: venv\n")
	return dst
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustNew(t *testing.T, opts Options) *Extractor {
	t.Helper()
	ex, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ex
}

func mustRun(t *testing.T, ex *Extractor) {
	t.Helper()
	if err := ex.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runToOut(t *testing.T, path, out string, opts Options) string {
	t.Helper()
	opts.Path = path
	opts.Out = out
	mustRun(t, mustNew(t, opts))
	return readFile(t, out)
}

func mustRunWithin(t *testing.T, ex *Extractor, d time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- ex.Run(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(d):
		t.Fatalf("Run hung after %s — likely missing out-path skip (scanning out file feeds itself)", d)
	}
}
