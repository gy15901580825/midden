# midden — AI session manager for Claude Code

Interactive TUI + CLI to browse, rename, clean and remove Claude Code sessions —
with a restorable trash, never a hard delete.

(A *midden* is the heap where old things pile up and get sifted through —
which is exactly what `~/.claude/projects` becomes.)

<GIF placeholder — record with vhs after v0.1.0>

## Why

Claude Code quietly accumulates session data under `~/.claude/projects`
(217 MB on the author's machine). Every conversation, every project you've
ever pointed it at, every stray sidecar directory — it all just sits there,
growing, with no built-in way to see what's taking the space or clean it up
short of poking around in the filesystem by hand. midden gives you a clear
picture of every project and session, how much space each one takes, and a
safe way to reclaim that space without ever risking data you didn't mean to
touch.

## Install

### Prebuilt binary (Linux / macOS / Windows)

Download the archive for your platform from the
[Releases](https://github.com/gy15901580825/midden/releases) page, unpack it,
and put the `midden` binary on your `PATH`.

The binaries are unsigned, so the first launch may be blocked:

- **macOS** — Gatekeeper says the developer can't be verified. Clear the
  quarantine flag once: `xattr -d com.apple.quarantine ./midden`
- **Windows** — SmartScreen shows a warning; choose *More info → Run anyway*.

### From source

Prerequisite: Go 1.22 or later (the build fetches the exact toolchain it
needs automatically).

    git clone https://github.com/gy15901580825/midden.git
    cd midden
    go build -o midden .

## Usage

    midden                     # interactive TUI
    midden projects            # list projects
    midden sessions <project>  # list sessions
    midden rename <id> "..."   # retitle a session
    midden remove <id>         # → trash (restorable)
    midden clear <project> --older-than 30d --empty --orphans
    midden trash list|restore <entry>|empty

All destructive commands support `--dry-run` and `--yes`. Add `--json` to
`projects`, `sessions`, and `trash list` for machine-readable output.

Project ids start with `-`; pass them after `--` so they aren't parsed as flags: `midden sessions -- -home-u-app`.

The default TUI (`midden` with no arguments) lets you browse projects and
sessions interactively, filter by name, multi-select, and drive the same
rename/remove/clear/preview flows with the keyboard.

## Safety model

- Every delete goes to `~/.local/share/midden/trash` with a manifest;
  `midden trash restore <entry>` puts the files back exactly where they came
  from.
- `memory/` directories are never touched by any command.
- Sessions active in the last 10 minutes are marked with ⚡ and are excluded
  from `clear` so you never yank a conversation out from under yourself.
- A path fence refuses to operate on anything outside
  `~/.claude/projects` — even a maliciously crafted project id or symlink
  can't make midden reach outside that tree.
- **No telemetry, no network calls, no accounts.** midden reads and moves
  local files and nothing else — it never phones home.

## Roadmap

- Codex (`~/.codex/sessions`) and Gemini CLI adapters — PRs welcome.
