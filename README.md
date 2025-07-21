# Apporte

**Apporte** – *"ah-**port**"* / **\[a.pɔʁt]** (French) — *to bring (something)*

A rule-based dispatcher written in Go.

Apporte is a modern, flexible alternative to `xdg-open`, `macOS open`, or Plan9's `plumber`. It allows you to define custom, regex-based dispatch rules per project, directory, or globally via simple TOML config files. Can be used for filepaths, URLs, strings and anything else.

## Features

- Cross-platform
- Regex-based rule matching
- Per project `.apporte.toml` support
- Group subsitution with `$0`, `$1`, etc
- Explain mode with the flag `--explain`
- Not relying on MIME databases or running daemons

## Example `.apporte.toml`

```toml
# Open URLs in the browser
[[rule]]
match = "^https://.*"
apporte = ["firefox", "$0"]

# Read manpages
[[rule]]
match = "([A-Za-z0-9])\\((\\d+)\\)"
apporte = ["man", "$2", "$1"]

# Search for Wiki articles
[[rule]]
match = "^wiki:(.+)$"
apporte = ["firefox", "https://en.wikipedia.org/wiki/Special:Search?search=$1"]

# Show commits in the repo
[[rule]]
match = "^[a-f0-9]{7,40}$"
apporte = ["git", "show", "$0"]

# Open a GitHub repo
[[rule]]
match = "^gh:([\\w-]+)/([\\w.-]+)$"
apporte = ["firefox", "https://github.com/$1/$2"]
```

## Usage

```shell
apporte README.md
apporte https://example.com
apporte wiki:Theory of relativity
apporte 9a8c3f2  # Git commit
apporte gh:torwals/linux
```

### CLI Flags

| Flag              | Description                             |
| ----------------- | --------------------------------------- |
| `-i`, `--input`   | Pass input directly (or via stdin)      |
| `-e`, `--explain` | Print matched rule and command, no exec |
| `-v`, `--verbose` | Like `--explain`, but runs the command  |
| `-c`, `--config`  | Add prioritized config file             |

## License

See [LICENSE](./LICENSE) for details.
