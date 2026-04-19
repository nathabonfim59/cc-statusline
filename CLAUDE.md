# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
go build -o claude-statusline .
echo '{"session_id":"test","model":{"name":"claude-sonnet-4-6"}}' | ./claude-statusline
```

No Makefile, CI, or test framework exists. No test files yet.

## Architecture

A stdin-to-stdout CLI filter that renders a two-line ANSI status bar for Claude Code sessions. Reads a JSON blob from stdin (session state from Claude Code) and prints a colored status bar to stdout.

**`main.go`** — Entry point and rendering. Defines the `Input` struct (JSON schema for Claude Code session data), renders model name, context window usage with progress bar, git info, cost, elapsed time, cache stats, rate limits, and diff stats.

**`theme.go`** — Theme engine. Embeds `themes/*.yaml` at compile time via `//go:embed`. Supports 3 color formats: named ANSI (`cyan`), hex (`#RRGGBB` → 24-bit true color), and raw ANSI escape passthrough. 5 semantic color roles: `primary`, `text`, `success`, `warning`, `danger`. Resolution order: `~/.config/claude-statusline/themes/<name>.yaml` → embedded built-in → hardcoded default.

**`themes/`** — YAML theme files (`default.yaml`, `onedark.yaml`, `monokai.yaml`, `catppuccin.yaml`).

Config lives at `~/.config/claude-statusline/config.yaml` with a `theme` field.

## Key Details

- Pure Go, single external dependency: `gopkg.in/yaml.v3`
- No CLI flags — driven entirely by stdin JSON and config file
- Git info is gathered by shelling out to `git -C <dir>` (not a Go git library)
- `min`/`max` helpers in `main.go` are redundant — Go 1.21+ provides these as builtins
