package extractor

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestXSkipsPatterns(t *testing.T) {
	sample, out := sampleWithOut(t)
	text := runToOut(t, sample, out, Options{
		Exclude:   []string{"skip_me.py", "vendor/"},
		Recursive: true,
	})
	mustNotContain(t, text, "TODO: skip", "TODO: vendored")
	mustContain(t, text, "TODO: main")
}

func TestSkipsHiddenAndDunder(t *testing.T) {
	sample, out := sampleWithOut(t)
	text := runToOut(t, sample, out, Options{Recursive: true})
	mustNotContain(t, text, "TODO: hidden", "TODO: dunder")
}

func TestHIncludesHiddenAndDunder(t *testing.T) {
	sample, out := sampleWithOut(t)
	text := runToOut(t, sample, out, Options{
		IncludeHidden: true,
		Recursive:     true,
	})
	mustContain(t, text, "TODO: hidden", "TODO: dunder")
}

func TestGAppliesGitignoreAndDefaultHidden(t *testing.T) {
	sample, out := sampleWithOut(t)
	text := runToOut(t, sample, out, Options{
		UseGitignore: true,
		Recursive:    true,
	})
	mustNotContain(t, text, "TODO: vendored", "TODO: skip", "TODO: hidden")
	mustContain(t, text, "TODO: main")
}

func TestGWithHIncludesHiddenButKeepsGitignore(t *testing.T) {
	sample, out := sampleWithOut(t)
	text := runToOut(t, sample, out, Options{
		UseGitignore:  true,
		IncludeHidden: true,
		Recursive:     true,
	})
	mustContain(t, text, "TODO: hidden", "TODO: venv")
	mustNotContain(t, text, "TODO: vendored")
}

func TestGWithXBangReincludesVendor(t *testing.T) {
	sample, out := sampleWithOut(t)
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
	sample, out := sampleWithOut(t)
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

func TestGetGitignorePatternsReadsSample(t *testing.T) {
	sample := materializeSample(t)
	lines, err := getGitignorePatterns(sample)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	mustContain(t, joined, "vendor/", "skip_me.py")
}
