package extractor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestCOmitsSnippetBody(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, filepath.Join(sample, "snippet.py"), out, Options{
		Concise: true,
	})
	mustContain(t, text, "FIXME")
	mustNotContain(t, text, "return 1")
}

func TestSnippetIncludesFollowingLines(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, filepath.Join(sample, "snippet.py"), out, Options{})
	mustContain(t, text, "return 1")
}

func TestWarnSnippetCount(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "warn.py")
	out := filepath.Join(dir, "out.txt")
	mustWrite(t, src, "# WARN{2} check this\nx = 1\ny = 2\n")
	text := runToOut(t, src, out, Options{
		Tokens:        []string{"WARN"},
		IgnoreDefault: true,
	})
	mustContain(t, text, "WARN", "x = 1")
	mustNotContain(t, text, "y = 2")
}

func TestSnippetPastEOFDoesNotCrash(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "short.py")
	out := filepath.Join(dir, "out.txt")
	mustWrite(t, src, "# TODO{10} almost eof\nonly one more\n")
	text := runToOut(t, src, out, Options{})
	mustContain(t, text, "TODO", "only one more")
}

func TestGetSnippetCountDefaultOne(t *testing.T) {
	line := "# TODO: x"
	start := strings.Index(line, "TODO") + len("TODO")
	n, err := snippetCount(line, start)
	if err != nil {
		t.Fatalf("snippetCount: %v", err)
	}
	if n != 1 {
		t.Fatalf("got %d, want 1", n)
	}
}

func TestGetSnippetCountParsesBraces(t *testing.T) {
	line := "# TODO{3} x"
	start := strings.Index(line, "TODO") + len("TODO")
	n, err := snippetCount(line, start)
	if err != nil {
		t.Fatalf("snippetCount: %v", err)
	}
	if n != 3 {
		t.Fatalf("got %d, want 3", n)
	}
}

func TestInvalidSnippetRaises(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantSub string
	}{
		{name: "unclosed", content: "# TODO{3 no close\n", wantSub: "unclosed"},
		{name: "non_int", content: "# TODO{abc}\n", wantSub: "abc"},
	}
	for _, concise := range []bool{false, true} {
		mode := "full"
		if concise {
			mode = "concise"
		}
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s_%s", tc.name, mode), func(t *testing.T) {
				dir := t.TempDir()
				src := filepath.Join(dir, "bad.py")
				out := filepath.Join(dir, "out.txt")
				mustWrite(t, src, tc.content)
				ex := mustNew(t, Options{Path: src, Out: out, Concise: concise})
				err := ex.Run(context.Background())
				if err == nil {
					t.Fatal("want error for invalid snippet count")
				}
				msg := err.Error()
				mustContain(t, msg, "invalid snippet count")
				if tc.wantSub != "" {
					mustContain(t, msg, tc.wantSub)
				}
			})
		}
	}
}

func TestInvalidSnippetDebugAddsFileLine(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantSub string
	}{
		{name: "unclosed", content: "# TODO{3 no close\n", wantSub: "unclosed"},
		{name: "non_int", content: "# TODO{abc}\n", wantSub: "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "bad.py")
			out := filepath.Join(dir, "out.txt")
			mustWrite(t, src, tc.content)
			ex := mustNew(t, Options{Path: src, Out: out, Debug: true})
			err := ex.Run(context.Background())
			if err == nil {
				t.Fatal("want error for invalid snippet count")
			}
			msg := err.Error()
			mustContain(t, msg, "invalid snippet count", tc.wantSub)
			mustContain(t, msg, "file", "line", "bad.py", "line 1:")
		})
	}
}
