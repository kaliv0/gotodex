<div align="center">
  <img src="https://github.com/kaliv0/gotodex/blob/main/.github/assets/truck.jpg?raw=true" width="400" alt="Gotodex">
</div>

# gotodex

Peak trash sniffer and TODO extractor

```bash
go install ./cmd/gtx
# or: make build -> bin/gtx

gtx <PATH> [OPTIONS...]
```

| Which | What |
| --- | --- |
| `PATH` | path to file or dir to process |
| `-h`, `--help` | show this help message and exit |
| `-x`, `--exclude PATTERN[,PATTERN...]` | git-like patterns to skip (comma-separated paths, names, or globs - will be combined with default .* / __*), e.g.<br><br><table><tr><td>bar.py</td><td>any file named bar.py</td></tr><tr><td>/bar.py</td><td>only top-level</td></tr><tr><td>foo/bar.py</td><td>path under the scan root</td></tr><tr><td>vendor/</td><td>directory and its contents</td></tr><tr><td>**/*.pyc</td><td>nested matches</td></tr><tr><td>!fizz.md</td><td>re-include after a broader exclude</td></tr></table> |
| `-H`, `--include-hidden` | do not apply default excludes for hidden names (.* / __*) |
| `-g`, `--use-gitignore` | use exclude patterns from .gitignore (if present in scan root) when traversing dirs |
| `-t`, `--tokens TOKEN[,TOKEN...]` | tokens to search (comma-separated - in addition to default TODO, FIXME)<br>e.g. `-t WARN,REVISIT` or `-t WARN -t REVISIT`.<br>If the token is followed by {lines-count} e.g. #FIXME{3}<br>the extractor will include multiline snippet with the length specified between the curly braces:<br><br>#FIXME{3} - revisit after release<br>if self.foo == "bar":<br>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;return f"fizz{buzz}" |
| `-i`, `--ignore-default` | use only -t tokens (skip default TODO, FIXME) |
| `-f`, `--full-path` | display absolute dir/file path |
| `-c`, `--concise` | display only token messages e.g. '#TODO{5}: after release', ignore longer snippets |
| `-r`, `--recursive` | traverse given dir path recursively |
| `-m`, `--max-depth N` | maximum depth of dir traversal - used with -r/--recursive flag (default: 120) |
| `-d`, `--debug` | enable debug mode |
| `-o`, `--out OUT` | path to output file - if existing dir is passed instead TODO out file will be saved inside the path |
| `-s`, `--stdout` | output result to stdout |
| `--tee` | output result to stdout and save to file |
| `-v`, `--version` | show program's version number and exit |

---

*Gotodex* is a Go rewrite of [todex](https://github.com/kaliv0/todex) (originally in Python).<br>
Parity target: Python todex **2.4.7**. See [FOLLOWUPS.md](FOLLOWUPS.md) for intentional deltas.<br>
The application code is written by hand. LLMs are used to maintain the test suite and optimize certain parts of the code base.

## Intentional deltas vs Python

- Binary name **`gtx`** only (no `todex` / `tdx` entrypoints). Unix-only.
- **`-t` / `-x`**: Cobra `StringSlice` accepts comma-separated values (and repeated flags). Python uses `nargs="+"`.
- Library **`MaxDepth <= 0` => 120**. Python library rejects `<= 0`. CLI still rejects non-positive `-m`.
- Token regex: Go RE2 has no lookbehind. Unicode word boundaries are checked in code.
- Pager: `PAGER` split on whitespace (no shell). `LESS=-RF` is set on the pager child only when unset. Process env is not mutated.
