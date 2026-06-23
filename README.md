# Session Search

**A fast way to find old conversations you had with Claude on your computer.**

If you've ever thought "I remember I talked to Claude about fixing that bug last month, but I have no idea where..." — this tool is for you.

## What it does, in plain words

Every time you chat with Claude (through Claude Code or the desktop app), it saves a record of that chat on your hard drive. These records are scattered in a few folders.

Session Search looks through all of them quickly and lets you:

- Type a few words you remember ("auth bug" or "todo list" or "how do I deploy")
- Instantly see matching conversations
- See them grouped by the project or folder you were working in at the time
- Pick one and get its file path (so you can open it, copy it, or feed it to another tool)

It feels a lot like using `rg` (ripgrep) or `fd` combined with `fzf`, but for your chat history.

## Why it exists

Claude sessions pile up. After a few months you have dozens or hundreds. Hunting through them manually is painful.

This tool makes it fast and pleasant, whether you're:
- Just trying to remember something
- Writing a script that needs to pull old context
- Building other tools/skills that want to reuse past conversations

## Two ways to use it

**1. Interactive (the pretty one)**

Just run:

```bash
session-search
# or start searching immediately
session-search "superpowers"
```

You get a nice live-updating list (like fzf). Results are grouped by project. Arrow keys or ctrl+j/k to move. Enter to pick.

**2. Command line (for scripts and other tools)**

```bash
# Get results as clean JSON
session-search --query "auth" --json

# Just the file paths
session-search "todo" --print path

# Limit results
session-search "plan" -n 5 --json
```

The JSON output makes it easy for other programs (or future "skills") to call this tool and get structured data.

## Future plans

The code is structured so we can add support for other AI tools later (Codex, Grok, etc.) without rewriting everything. Each "provider" knows how to find and read its own sessions.

## Performance

The goal is to feel as fast as ripgrep when searching text and fd when listing files.

It does this by:
- Walking directories in parallel
- Only reading the information it actually needs (not the entire chat history)
- Using ripgrep (if you have it) for the absolute fastest possible text matching
- Keeping things lightweight in memory

On a normal machine with a normal amount of Claude history, searches are basically instant.

## Installation

```bash
cd /path/to/session-search
make build
# or
go build -o ~/.local/bin/session-search ./cmd/session-search
```

Make sure `~/.local/bin` is in your PATH.

## Tips

- The more you use Claude, the more useful this becomes.
- Combine with other tools: `cat $(session-search "bug" --print path | head -1)`
- Use `--json` when writing scripts or other AI tools that want context from your past chats.

That's it. Type what you remember, and it finds the chat. Simple.