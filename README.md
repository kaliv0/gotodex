<div align="center">
  <img src="https://github.com/kaliv0/gotodex/blob/main/.github/assets/truck.jpg?raw=true" width="400" alt="Gotodex">
</div>

<a href="https://github.com/kaliv0/gotodex/releases"><img src="https://img.shields.io/github/release/kaliv0/gotodex.svg" alt="Latest Release"></a>
<a href="https://pkg.go.dev/github.com/kaliv0/gotodex"><img src="https://pkg.go.dev/badge/github.com/kaliv0/gotodex.svg" alt="pkg.go.dev"></a>
<a href="https://github.com/kaliv0/gotodex/actions"><img src="https://github.com/kaliv0/gotodex/actions/workflows/ci.yml/badge.svg?branch=main" alt="Build Status"></a>

# gotodex

Peak trash sniffer and TODO extractor

```bash
go install github.com/kaliv0/gotodex/cmd/gtx@latest
```
Usage:

```bash
gtx <PATH> [OPTIONS...]
```

| Which | What |
| --- | --- |
| `PATH` | path to file or dir to process |
| `-h`, `--help` | show this help message and exit |
| `-x`, `--exclude PATTERN[,PATTERN...]` | git-like patterns to skip (comma-separated paths, names, or globs - will be combined with default .* / __*), e.g.<br><br><table><tr><td>bar.go</td><td>any file named bar.go</td></tr><tr><td>/bar.go</td><td>only top-level</td></tr><tr><td>foo/bar.go</td><td>path under the scan root</td></tr><tr><td>vendor/</td><td>directory and its contents</td></tr><tr><td>**/*.o</td><td>nested matches</td></tr><tr><td>!fizz.md</td><td>re-include after a broader exclude</td></tr></table> |
| `-H`, `--include-hidden` | do not apply default excludes for hidden names (.* / __*) |
| `-g`, `--use-gitignore` | use exclude patterns from .gitignore (if present in scan root) when traversing dirs |
| `-t`, `--tokens TOKEN[,TOKEN...]` | tokens to search (comma-separated - in addition to default TODO, FIXME)<br>e.g. `-t WARN,REVISIT` or `-t WARN -t REVISIT`.<br>If the token is followed by {lines-count} e.g. //FIXME{3}<br>the extractor will include multiline snippet with the length specified between the curly braces:<br><br>//FIXME{3} - revisit after release<br>if path == root {<br>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;return false<br>} |
| `-i`, `--ignore-default` | use only -t tokens (skip default TODO, FIXME) |
| `-f`, `--full-path` | display absolute dir/file path |
| `-c`, `--concise` | display only token messages e.g. '//TODO{5}: after release', ignore longer snippets |
| `-r`, `--recursive` | traverse given dir path recursively |
| `-m`, `--max-depth N` | maximum depth of dir traversal - used with -r/--recursive flag (default: 120) |
| `-d`, `--debug` | enable debug mode |
| `-o`, `--out OUT` | path to output file - if existing dir is passed instead TODO out file will be saved inside the path |
| `-s`, `--stdout` | output result to stdout |
| `--tee` | output result to stdout and save to file |
| `-v`, `--version` | show program's version number and exit |

---

*Gotodex* is a Go rewrite of [todex](https://github.com/kaliv0/todex).
