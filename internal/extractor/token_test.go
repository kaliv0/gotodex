package extractor

import (
	"regexp"
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
		if got := matched(m, "前中文标记后"); got != "" {
			t.Fatalf("expected no match inside word, got %q", got)
		}
	})
	t.Run("overlap_leftmost_longest", func(t *testing.T) {
		m := pattern(t, []string{"AB", "ABC", "BC"}, true)
		if got := matched(m, " ABC "); got != "ABC" {
			t.Fatalf("got %q want ABC", got)
		}
	})
}
