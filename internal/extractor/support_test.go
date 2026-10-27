package extractor

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func pattern(t *testing.T, tokens []string, ignoreDefault bool) *regexp.Regexp {
	t.Helper()
	re, err := prepareTokenPattern(tokens, ignoreDefault)
	if err != nil {
		t.Fatalf("prepareTokenPattern: %v", err)
	}
	return re
}

func matched(re *regexp.Regexp, line string) string {
	hit := findToken(re, line)
	if hit == nil {
		return ""
	}
	return hit.Token
}

func TestEmptyWithIgnoreDefaultRaises(t *testing.T) {
	_, err := prepareTokenPattern(nil, true)
	if err == nil || err.Error() != "no tokens to search for" {
		t.Fatalf("want no tokens error, got %v", err)
	}
}

func TestPrepareTokenPattern(t *testing.T) {
	tests := []struct {
		tokens        []string
		ignoreDefault bool
		line          string
		expected      string
	}{
		{nil, false, "# TODO: x", "TODO"},
		{nil, false, "# FIXME: x", "FIXME"},
		{[]string{"WARN"}, false, "# WARN: x", "WARN"},
		{[]string{"WARN"}, false, "# TODO: x", "TODO"},
		{[]string{"WARN"}, true, "# WARN: x", "WARN"},
		{[]string{"WARN"}, true, "# TODO: x", ""},
		{[]string{"WARN"}, true, "# FIXME: x", ""},
		{[]string{"WARN", "REVISIT"}, true, "WARN here", "WARN"},
		{[]string{"WARN", "REVISIT"}, true, "REVISIT later", "REVISIT"},
		{[]string{"TODO", "TODO", "FIXME"}, false, "TODO", "TODO"},
		{[]string{"TODO", "TODO", "FIXME"}, false, "FIXME", "FIXME"},
	}
	for _, tt := range tests {
		got := matched(pattern(t, tt.tokens, tt.ignoreDefault), tt.line)
		if got != tt.expected {
			t.Fatalf("tokens=%v ignore=%v line=%q: got %q want %q",
				tt.tokens, tt.ignoreDefault, tt.line, got, tt.expected)
		}
	}
}

func TestWordBoundariesAndCase(t *testing.T) {
	m := pattern(t, nil, false)
	tests := []struct {
		line, expected string
	}{
		{"todo: lower", "todo"},
		{"Todo: mixed", "Todo"},
		{"fIxMe: weird", "fIxMe"},
		{"TODO", "TODO"},
		{"#TODO: x", "TODO"},
		{"//FIXME x", "FIXME"},
		{"(TODO)", "TODO"},
		{"[FIXME]", "FIXME"},
		{"please TODO this soon", "TODO"},
		{"TODOS remaining", ""},
		{"MYTODO done", ""},
		{"XXTODOYY", ""},
	}
	for _, tt := range tests {
		got := matched(m, tt.line)
		if got != tt.expected {
			t.Fatalf("line=%q: got %q want %q", tt.line, got, tt.expected)
		}
	}
}

func TestPrefersLongerToken(t *testing.T) {
	m := pattern(t, []string{"FIX", "FIXME"}, true)
	if got := matched(m, "FIXME: later"); got != "FIXME" {
		t.Fatalf("got %q want FIXME", got)
	}
	if got := matched(m, "FIX: now"); got != "FIX" {
		t.Fatalf("got %q want FIX", got)
	}
}

func TestSpecialCharactersAreLiteral(t *testing.T) {
	tests := []struct {
		tokens   []string
		line     string
		expected string
	}{
		{[]string{"C++"}, "use C++ here", "C++"},
		{[]string{"file.txt"}, "see file.txt now", "file.txt"},
		{[]string{"file.txt"}, "fileXtxt", ""},
		{[]string{"a.b*"}, "a.b* value", "a.b*"},
		{[]string{"A+"}, "A+", "A+"},
		{[]string{"A+"}, "AAA", ""},
		{[]string{"@FIXME"}, "# @FIXME: x", "@FIXME"},
		{[]string{"@FIXME"}, "# FIXME: bare", ""},
		{[]string{"Do we need this!"}, "# Do we need this!", "Do we need this!"},
		{[]string{"!!!URGENT!!!"}, "# !!!URGENT!!!", "!!!URGENT!!!"},
	}
	for _, tt := range tests {
		got := matched(pattern(t, tt.tokens, true), tt.line)
		if got != tt.expected {
			t.Fatalf("tokens=%v line=%q: got %q want %q", tt.tokens, tt.line, got, tt.expected)
		}
	}
}

func TestTokenEdgeCases(t *testing.T) {
	t.Run("non_ascii_token", func(t *testing.T) {
		m := pattern(t, []string{"中文标记"}, true)
		if got := matched(m, "# 中文标记: x"); got != "中文标记" {
			t.Fatalf("got %q", got)
		}
		// Adjacent CJK letter is a word char — should not match mid-word.
		if got := matched(m, "前中文标记后"); got != "" {
			t.Fatalf("expected no match inside word, got %q", got)
		}
	})
	t.Run("overlap_leftmost_longest", func(t *testing.T) {
		m := pattern(t, []string{"AB", "ABC", "BC"}, true)
		// space before A so \w-boundary allows a match; prefer longest alt
		if got := matched(m, " ABC "); got != "ABC" {
			t.Fatalf("got %q want ABC", got)
		}
	})
}

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
	// Call with token-end index (after token).
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
	// Token-end index, not Index("{").
	start := strings.Index(line, "TODO") + len("TODO")
	n, err := snippetCount(line, start)
	if err != nil {
		t.Fatalf("snippetCount: %v", err)
	}
	if n != 3 {
		t.Fatalf("got %d, want 3", n)
	}
}

// Invalid {N} errors in both modes with a unified message.
func TestInvalidSnippetRaises(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantSub string // beyond "invalid snippet count"
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

func TestXSkipsPatterns(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{
		Exclude:   []string{"skip_me.py", "vendor/"},
		Recursive: true,
	})
	mustNotContain(t, text, "TODO: skip", "TODO: vendored")
	mustContain(t, text, "TODO: main")
}

func TestSkipsHiddenAndDunder(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{Recursive: true})
	mustNotContain(t, text, "TODO: hidden", "TODO: dunder")
}

func TestHIncludesHiddenAndDunder(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{
		IncludeHidden: true,
		Recursive:     true,
	})
	mustContain(t, text, "TODO: hidden", "TODO: dunder")
}

func TestGAppliesGitignoreAndDefaultHidden(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{
		UseGitignore: true,
		Recursive:    true,
	})
	mustNotContain(t, text, "TODO: vendored", "TODO: skip", "TODO: hidden")
	mustContain(t, text, "TODO: main")
}

func TestGWithHIncludesHiddenButKeepsGitignore(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{
		UseGitignore:  true,
		IncludeHidden: true,
		Recursive:     true,
	})
	mustContain(t, text, "TODO: hidden", "TODO: venv")
	mustNotContain(t, text, "TODO: vendored")
}

func TestGWithXBangReincludesVendor(t *testing.T) {
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	text := runToOut(t, sample, out, Options{
		UseGitignore: true,
		Exclude:      []string{"!vendor/", "!vendor/**"},
		Recursive:    true,
	})
	mustContain(t, text, "TODO: vendored")
}

func TestGMissingGitignoreIsNoop(t *testing.T) {
	sample := materializeSample(t)
	bare := filepath.Join(sample, "bare")
	out := filepath.Join(bare, "out.txt")
	text := runToOut(t, bare, out, Options{UseGitignore: true})
	mustContain(t, text, "TODO: bare")
}

func TestFilePathGIsNoopButStillProcesses(t *testing.T) {
	// -g only loads .gitignore for directory scans → explicit file PATH always processed
	sample := materializeSample(t)
	out := filepath.Join(sample, "out.txt")
	ex := mustNew(t, Options{
		Path:         filepath.Join(sample, "app.py"),
		UseGitignore: true,
		Out:          out,
	})
	patterns, err := getGitignorePatterns(ex.root)
	if err != nil {
		t.Fatalf("getGitignorePatterns: %v", err)
	}
	if len(patterns) != 0 {
		t.Fatalf("file PATH getGitignorePatterns want [], got %#v", patterns)
	}

	text := runToOut(t, filepath.Join(sample, "app.py"), out, Options{UseGitignore: true})
	mustContain(t, text, "TODO: main")
}

func TestPrepareExcludeSpecDefaultGlobs(t *testing.T) {
	gi, err := prepareExcludeSpec(t.TempDir(), nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".hidden.py", "__init__.py", ".venv/x.py"} {
		if !excludeMatch(gi, rel, false) {
			t.Errorf("DefaultGlobs must exclude %q", rel)
		}
	}
	if excludeMatch(gi, "app.py", false) {
		t.Error("app.py must not be excluded by defaults")
	}
	if !excludeMatch(gi, ".venv", true) {
		t.Error("dir .venv must match .* (check rel and rel+/)")
	}
}

func TestPrepareExcludeSpecXPatterns(t *testing.T) {
	gi, err := prepareExcludeSpec(t.TempDir(), []string{"skip_me.py", "vendor/"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !excludeMatch(gi, "skip_me.py", false) {
		t.Error("skip_me.py")
	}
	if !excludeMatch(gi, "vendor/lib.py", false) {
		t.Error("vendor/lib.py under vendor/")
	}
	if !excludeMatch(gi, "vendor", true) {
		t.Error("dir vendor must match vendor/ via trailing-slash check")
	}
	if excludeMatch(gi, "app.py", false) {
		t.Error("app.py must not match")
	}
}

func TestPrepareExcludeSpecBangOrder(t *testing.T) {
	// Last gitignore-style rule wins; -x bang after gitignore vendor/.
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "vendor/\nskip_me.py\n")
	gi, err := prepareExcludeSpec(dir, []string{"!vendor/", "!vendor/**"}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if excludeMatch(gi, "vendor/lib.py", false) {
		t.Error("!vendor/ must re-include vendor/lib.py")
	}
	if excludeMatch(gi, "vendor", true) {
		t.Error("!vendor/ must re-include dir vendor")
	}
	if !excludeMatch(gi, "skip_me.py", false) {
		t.Error("skip_me.py still excluded from gitignore")
	}
}

func TestPrepareExcludeSpecIgnoresBlankAndCommentLines(t *testing.T) {
	// Blank / # / whitespace lines are no-op patterns.
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "# comment\n\n   \nonly_me.py\n")
	gi, err := prepareExcludeSpec(dir, nil, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !excludeMatch(gi, "only_me.py", false) {
		t.Error("only_me.py must be excluded")
	}
	if excludeMatch(gi, "app.py", false) {
		t.Error("comment/blank must not exclude app.py")
	}
}

func TestExtraGetGitignorePatternsReadsSample(t *testing.T) {
	sample := materializeSample(t)
	lines, err := getGitignorePatterns(sample)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	mustContain(t, joined, "vendor/", "skip_me.py")
}

func TestExtraDefaultGlobsExported(t *testing.T) {
	want := []string{".*", "__*"}
	if len(DefaultGlobs) != 2 || DefaultGlobs[0] != want[0] || DefaultGlobs[1] != want[1] {
		t.Fatalf("DefaultGlobs=%#v want %#v", DefaultGlobs, want)
	}
}
