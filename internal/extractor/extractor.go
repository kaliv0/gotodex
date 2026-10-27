package extractor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	ignore "github.com/sabhiram/go-gitignore"

	"gotodex/internal/writer"
)

// Extractor scans PATH for tokens and writes results.
type Extractor struct {
	StdoutWriter io.Writer // -s/--tee sink; nil => os.Stdout (test seam)
	StderrWriter io.Writer // Writer.Error sink; nil => os.Stderr (test seam)
	re           *regexp.Regexp
	gi           *ignore.GitIgnore
	out          *writer.Writer
	afterFile    func(path string) // after each non-excluded file (mid-walk cancel seam)
	root         string            // filepath.Clean(opts.Path); root-equality exclude guard
	outAbs       string            // filepath.Abs(out.Path) once; empty ⇒ no out-file skip
	opts         Options
}

// New validates options and prepares the token regexp and exclude spec.
func New(opts Options) (*Extractor, error) {
	re, err := prepareTokenPattern(opts.Tokens, opts.IgnoreDefault)
	if err != nil {
		return nil, err
	}
	gi, err := prepareExcludeSpec(opts.Path, opts.Exclude, opts.IncludeHidden, opts.UseGitignore)
	if err != nil {
		return nil, err
	}
	return &Extractor{
		opts: opts,
		re:   re,
		gi:   gi,
		root: filepath.Clean(opts.Path),
	}, nil
}

// Run extracts tokens from opts.Path into the configured out writer.
// Cancel is cooperative: checked here and at processFile/processDir entry;
// an in-flight file read still finishes. nil ctx ⇒ context.Background().
func (e *Extractor) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Early return avoids Stat/setOut when already canceled.
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.opts.Path == "" {
		return fmt.Errorf("path required")
	}
	info, err := os.Stat(e.opts.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s not found", e.opts.Path)
		}
		return err
	}

	if err := e.setOut(); err != nil {
		return err
	}
	defer e.out.Close()

	if err := e.debug("processing:\n"); err != nil {
		return err
	}

	// Do not swallow StopExtraction: CLI maps it → exit 141.
	switch {
	case info.Mode().IsRegular():
		return e.processFile(ctx, e.opts.Path)
	case info.IsDir():
		return e.processDir(ctx, e.opts.Path, 0)
	default:
		return fmt.Errorf("%s must be path to file or dir", e.opts.Path)
	}
}

// setOut installs the out writer and caches Abs(Path) once for out-file skip.
// Propagates Writer open errors (Path set ⇒ create/truncate immediately).
func (e *Extractor) setOut() error {
	path, toStdout := "", true
	if !e.opts.Stdout {
		name := e.opts.Out
		if name == "" {
			name = defaultOutName
		}
		if fi, err := os.Stat(name); err == nil && fi.IsDir() {
			name = filepath.Join(name, defaultOutName)
		}
		path, toStdout = name, e.opts.Tee
	}
	tty := false
	if toStdout && e.StdoutWriter == nil && e.StderrWriter == nil {
		if fi, err := os.Stdout.Stat(); err == nil {
			tty = (fi.Mode() & os.ModeCharDevice) != 0
		}
	}
	w, err := writer.NewWith(writer.Options{
		Path:     path,
		ToStdout: toStdout,
		TTY:      tty,
		Stdout:   e.StdoutWriter,
		Stderr:   e.StderrWriter,
	})
	if err != nil {
		return err
	}
	e.out = w
	e.outAbs = ""
	if w.Path != "" {
		e.outAbs, _ = filepath.Abs(w.Path) // Abs err ⇒ ""; skip out-file match
	}
	return nil
}

func (e *Extractor) debug(msg string) error {
	if !e.opts.Debug {
		return nil
	}
	return e.out.Debug(msg)
}

// processDir walks dir: exclude check, then depth > maxDepth gate; descend
// only when Recursive. DirEntry is Lstat-based (no symlink follow). Files
// then dirs, ReadDir name order.
func (e *Extractor) processDir(ctx context.Context, dir string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	maxDepth := e.opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	if e.isExcluded(dir, true) || depth > maxDepth {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var dirs []string
	for _, ent := range entries {
		switch {
		case ent.IsDir():
			if e.opts.Recursive {
				dirs = append(dirs, filepath.Join(dir, ent.Name()))
			}
		case ent.Type().IsRegular():
			if err := e.processFile(ctx, filepath.Join(dir, ent.Name())); err != nil {
				return err
			}
		}
	}
	for _, child := range dirs {
		if err := e.processDir(ctx, child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (e *Extractor) processFile(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.isExcluded(path, false) {
		return nil
	}
	if e.afterFile != nil {
		defer e.afterFile(path)
	}

	if err := e.debug("\t" + e.pathName(path) + "\n"); err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	lineNo := 0
	headerAdded := false

	for {
		line, readErr := br.ReadString('\n')
		if line != "" {
			lineNo++
			// Invalid UTF-8 → abort file, keep prior hits; Error only when Debug.
			if !utf8.ValidString(line) {
				return e.reportSkipUndecodable(path, utf8DecodeErr(line))
			}
			if m := findToken(e.re, line); m != nil {
				lineNo, headerAdded, err = e.writeHit(br, path, line, lineNo, m, headerAdded)
				if err != nil {
					return err
				}
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// reportSkipUndecodable emits Writer.Error when Debug; never fails the whole run for the skip itself.
func (e *Extractor) reportSkipUndecodable(path string, cause error) error {
	if !e.opts.Debug {
		return nil
	}
	return e.out.Error(fmt.Sprintf("skipping %s:\n%s\n", e.pathName(path), cause))
}

// utf8DecodeErr builds a decode-error message for the first bad byte.
// Caller must pass a string that failed utf8.ValidString (always finds a bad byte).
func utf8DecodeErr(s string) error {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		i += size
	}
	return fmt.Errorf("'utf-8' codec can't decode byte 0x%02x in position %d: invalid start byte", s[i], i)
}

// writeHit emits one token hit (header once, then concise slice or expanded snippet).
// Invalid UTF-8 in a snippet-consumed line aborts the file via reportSkipUndecodable.
func (e *Extractor) writeHit(br *bufio.Reader, path, line string, lineNo int, m *tokenHit, headerAdded bool) (int, bool, error) {
	if !headerAdded {
		if err := e.out.WriteHeader(e.pathName(path)); err != nil {
			return lineNo, false, err
		}
		headerAdded = true
	}
	// Validate {N} under concise too (no expand; still slice from token).
	n, err := snippetCount(line, m.End)
	if err != nil {
		return lineNo, headerAdded, err
	}
	if e.opts.Concise {
		return lineNo, headerAdded, e.out.WriteEntry(lineNo, line[m.Start:])
	}
	displayLine := lineNo
	var bad string
	line, lineNo, bad = expand(br, line, lineNo, n)
	if bad != "" {
		return lineNo, headerAdded, e.reportSkipUndecodable(path, utf8DecodeErr(bad))
	}
	// Multi-line shape keyed off lines actually read, not parsed N.
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	if displayLine != lineNo {
		// Leading "\n" → blank after "line N:"; body double-tab indented.
		line = indent(dedent("\n"+line), "\t\t")
	} else {
		line = dedent(line)
	}
	return lineNo, headerAdded, e.out.WriteEntry(displayLine, line)
}

func (e *Extractor) pathName(path string) string {
	if e.opts.FullPath {
		abs, err := filepath.Abs(path)
		if err != nil {
			return path
		}
		return abs
	}
	if e.opts.Recursive {
		return path
	}
	return filepath.Base(path)
}
