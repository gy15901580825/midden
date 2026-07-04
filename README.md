# aism — AI Session Manager

Interactive TUI + CLI to browse, rename, clean and remove Claude Code sessions —
with a restorable trash, never a hard delete.

<GIF placeholder — record with vhs after v0.1.0>

## Why

Claude Code quietly accumulates session data under `~/.claude/projects`
(217 MB on the author's machine). Every conversation, every project you've
ever pointed it at, every stray sidecar directory — it all just sits there,
growing, with no built-in way to see what's taking the space or clean it up
short of poking around in the filesystem by hand. aism gives you a clear
picture of every project and session, how much space each one takes, and a
safe way to reclaim that space without ever risking data you didn't mean to
touch.

## Install

Prerequisite: Go 1.22 or later (the build fetches the exact toolchain it
needs automatically).

The module isn't published yet, so `go install` isn't available. Build from
source instead:

    git clone https://github.com/<you>/aism.git
    cd aism
    go build -o aism .

Once released, prebuilt binaries for Linux, macOS and Windows will be
attached to each GitHub release (see `.goreleaser.yaml`), and `go install`
will work once the module path is finalized.

## Usage

    aism                     # interactive TUI
    aism projects            # list projects
    aism sessions <project>  # list sessions
    aism rename <id> "..."   # retitle a session
    aism remove <id>         # → trash (restorable)
    aism clear <project> --older-than 30d --empty --orphans
    aism trash list|restore <entry>|empty

All destructive commands support `--dry-run` and `--yes`. Add `--json` to
`projects`, `sessions`, and `trash list` for machine-readable output.

Project ids start with `-`; pass them after `--` so they aren't parsed as flags: `aism sessions -- -home-u-app`.

The default TUI (`aism` with no arguments) lets you browse projects and
sessions interactively, filter by name, multi-select, and drive the same
rename/remove/clear/preview flows with the keyboard.

## Safety model

- Every delete goes to `~/.local/share/aism/trash` with a manifest;
  `aism trash restore <entry>` puts the files back exactly where they came
  from.
- `memory/` directories are never touched by any command.
- Sessions active in the last 10 minutes are marked with ⚡ and are excluded
  from `clear` so you never yank a conversation out from under yourself.
- A path fence refuses to operate on anything outside
  `~/.claude/projects` — even a maliciously crafted project id or symlink
  can't make aism reach outside that tree.
- **No telemetry, no network calls, no accounts.** aism reads and moves
  local files and nothing else — it never phones home.

## Roadmap

- Codex (`~/.codex/sessions`) and Gemini CLI adapters — PRs welcome.
