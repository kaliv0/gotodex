package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"gotodex/internal/extractor"
	"gotodex/internal/writer"
)

// Version is the CLI --version string.
const Version = "0.1.0"

const shortDesc = `Peak trash sniffer and TODO extractor`

const banner = `
       ________________   ___/-\___     ___/-\___     ___/-\___
     / /             ||  |---------|   |---------|   |---------|
    / /              ||   |       |     | | | | |     |   |   |
   / /             __||   |       |     | | | | |     | | | | |
  / /   \\        I  ||   |       |     | | | | |     | | | | |
 (-------------------||   | | | | |     | | | | |     | | | | |
 ||               == ||   |_______|     |_______|     |_______|
 ||        GTX       | =============================================
 ||          ____    |                                ____      |
( | o      / ____ \                                 / ____ \    |)
 ||      / / . . \ \                              / / . . \ \   |
[ |_____| | .   . | |____________________________| | .   . | |__]
          | .   . |                                | .   . |
           \_____/                                  \_____/

            ` + shortDesc + `
`

var errInterrupted = errors.New("interrupted")

type flagBundle struct {
	out           string
	tokens        []string
	exclude       []string
	maxDepth      int
	concise       bool
	fullPath      bool
	useGitignore  bool
	recursive     bool
	includeHidden bool
	maxDepthSet   bool
	debug         bool
	ignoreDefault bool
	stdout        bool
	tee           bool
}

// NewRoot builds the gtx Cobra command.
func NewRoot() *cobra.Command {
	return newRoot(nil, nil)
}

func newRoot(stdout, stderr io.Writer) *cobra.Command {
	f := &flagBundle{}

	cmd := &cobra.Command{
		Use:           "gtx PATH",
		Short:         shortDesc,
		Long:          banner,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return &exitError{Code: 2, Err: err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validate(cmd, f); err != nil {
				return &exitError{Code: 2, Err: err}
			}
			opts := extractor.Options{
				Path:          args[0],
				Tokens:        f.tokens,
				IgnoreDefault: f.ignoreDefault,
				Exclude:       f.exclude,
				IncludeHidden: f.includeHidden,
				UseGitignore:  f.useGitignore,
				FullPath:      f.fullPath,
				Concise:       f.concise,
				Recursive:     f.recursive,
				MaxDepth:      f.maxDepth,
				Debug:         f.debug,
				Out:           f.out,
				Stdout:        f.stdout,
				Tee:           f.tee,
			}
			ex, err := extractor.New(opts)
			if err != nil {
				return err
			}
			ex.StdoutWriter = stdout
			ex.StderrWriter = stderr
			return runInterruptible(ex.Run)
		},
	}

	cmd.Flags().StringSliceVarP(&f.tokens, "tokens", "t", nil, "tokens to search (comma-separated - defaults TODO,FIXME)")
	cmd.Flags().BoolVarP(&f.ignoreDefault, "ignore-default", "i", false, "use only -t tokens")
	cmd.Flags().StringSliceVarP(&f.exclude, "exclude", "x", nil, "gitignore-style exclude patterns (comma-separated)")
	cmd.Flags().BoolVarP(&f.includeHidden, "include-hidden", "H", false, "do not apply default .* / __* excludes")
	cmd.Flags().BoolVarP(&f.useGitignore, "use-gitignore", "g", false, "apply .gitignore from scan root")
	cmd.Flags().BoolVarP(&f.fullPath, "full-path", "f", false, "display absolute paths")
	cmd.Flags().BoolVarP(&f.concise, "concise", "c", false, "token messages only; no snippet body")
	cmd.Flags().BoolVarP(&f.recursive, "recursive", "r", false, "traverse directories recursively")
	cmd.Flags().IntVarP(&f.maxDepth, "max-depth", "m", 0, "max dir depth (requires -r)")
	cmd.Flags().BoolVarP(&f.debug, "debug", "d", false, "enable debug mode")
	cmd.Flags().StringVarP(&f.out, "out", "o", "", "output file (or dir to place TODO inside)")
	cmd.Flags().BoolVarP(&f.stdout, "stdout", "s", false, "write to stdout only")
	cmd.Flags().BoolVar(&f.tee, "tee", false, "write to stdout and file")

	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &exitError{Code: 2, Err: err}
	})
	cmd.PreRun = func(cmd *cobra.Command, _ []string) {
		f.maxDepthSet = cmd.Flags().Changed("max-depth")
		f.tokens = trimNonEmpty(f.tokens)
		f.exclude = trimNonEmpty(f.exclude)
	}
	cmd.SetVersionTemplate("gtx version {{.Version}}\n")

	return cmd
}

func validate(cmd *cobra.Command, f *flagBundle) error {
	if f.ignoreDefault && len(f.tokens) == 0 {
		return fmt.Errorf("%s flag requires %s", flagNames(cmd, "ignore-default"), flagNames(cmd, "tokens"))
	}
	if f.maxDepthSet && !f.recursive {
		return fmt.Errorf("%s flag requires %s", flagNames(cmd, "max-depth"), flagNames(cmd, "recursive"))
	}
	if f.maxDepthSet && f.maxDepth <= 0 {
		return fmt.Errorf("argument -m/--max-depth: expected positive value")
	}
	if f.stdout && (f.out != "" || f.tee) {
		return fmt.Errorf("shouldn't mix --stdout and --out or --tee flags")
	}
	return nil
}

func trimNonEmpty(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func flagNames(cmd *cobra.Command, name string) string {
	flg := cmd.Flags().Lookup(name)
	if flg == nil {
		return name
	}
	if flg.Shorthand != "" {
		return fmt.Sprintf("-%s/--%s", flg.Shorthand, name)
	}
	return "--" + name
}

// Execute runs root and maps errors to exit codes.
func Execute() int {
	return finish(NewRoot().Execute())
}

func runRoot(args []string, stdout, stderr io.Writer) error {
	root := newRoot(stdout, stderr)
	root.SetArgs(args)
	if stdout != nil {
		root.SetOut(stdout)
	}
	if stderr != nil {
		root.SetErr(stderr)
	}
	return root.Execute()
}

// finish maps err to an exit code.
// Quiet on 0/130/141. Prefix usage (2). Bare message on runtime (1).
func finish(err error) int {
	code := exitCode(err)
	switch code {
	case 0, 130, 141:
		return code
	case 2:
		fmt.Fprintln(os.Stderr, "gtx: error:", err.Error())
	default:
		fmt.Fprintln(os.Stderr, err.Error())
	}
	return code
}

// exitCode: 0 ok, 2 usage, 1 runtime, 141 EPIPE, 130 interrupt.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := errors.AsType[*exitError](err); ok {
		return exitErr.Code
	}
	if errors.Is(err, errInterrupted) {
		return 130
	}
	if _, ok := errors.AsType[writer.StopExtraction](err); ok || writer.IsBrokenPipe(err) {
		return 141
	}
	return 1
}

func runInterruptible(fn func(context.Context) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := fn(ctx)
	if errors.Is(err, context.Canceled) {
		return errInterrupted
	}
	return err
}

type exitError struct {
	Err  error
	Code int
}

func (e *exitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *exitError) Unwrap() error { return e.Err }
