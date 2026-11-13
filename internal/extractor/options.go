package extractor

import "io"

const (
	// DefaultMaxDepth is used when MaxDepth is <= 0.
	DefaultMaxDepth = 120
	defaultOutName  = "TODO"
)

// DefaultTokens are the built-in search tokens.
var DefaultTokens = []string{"TODO", "FIXME"}

// DefaultGlobs exclude hidden / dunder names unless -H.
var DefaultGlobs = []string{".*", "__*"}

// Options configures extraction.
type Options struct {
	Path          string
	Out           string // empty => defaultOutName when not stdout-only
	Tokens        []string
	Exclude       []string
	MaxDepth      int       // <= 0 => DefaultMaxDepth (normalized in New)
	Stdout        bool      // -s stdout-only
	StdoutW       io.Writer // nil => os.Stdout. *W avoids clash with Stdout bool
	StderrW       io.Writer // nil => os.Stderr
	UseGitignore  bool
	Tee           bool
	IncludeHidden bool
	FullPath      bool
	Concise       bool
	Recursive     bool
	IgnoreDefault bool
	Debug         bool
}
