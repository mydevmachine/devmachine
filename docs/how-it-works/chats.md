# Chats

`devmachine chat` talks to the coding CLI you are already logged in to,
`claude` or `codex`, on one of your workspaces or on this computer. The
macOS app's Chats mode, its quick panel and the Ask widget all go through
it, and so can a terminal.

## The CLI, not an API

A chat never calls a model's API and never asks for a key. It runs the same
`claude` or `codex` you use in a session, logged in with your own
subscription. If that CLI works when you type it on the workspace, a chat
works there too. If it is not logged in, the chat says so
(`harness-logged-out`), and the fix is to log in there once.

## One process per message

Each message starts the harness, sends the message, streams the answer and
ends. No process waits between messages. The next message continues the
conversation by the harness's own session id: `claude --resume <id>` or
`codex exec resume <id>`. The harness keeps the conversation in its own
transcript on the workspace; that transcript is what the model sees.

So a chat survives a closed laptop, an app restart or a dropped connection:
there is nothing to keep alive.

## Read only

A chat cannot change your files. claude runs with `--permission-mode plan`
and codex with `--sandbox read-only`. This is fixed, not a setting: coding
happens in a session, and a chat is for questions.

## What read only means

Read only is the harness's own rule, so it is worth knowing where it stops.

- claude runs in `--permission-mode plan`, so its own tools that edit files
  are refused.
- Every other tool follows the workspace's own Claude settings: its MCP
  servers, which load on their own, and its permission allow list. On
  2026-10-16 an MCP tool that only reads (Linear's list_teams) ran in a
  chat, and so did a Bash command that only reads.
- A tool that would need your approval is refused, since a chat has nobody
  to ask. claude lists those at the end of its answer; the chat passes them
  on as `denied` lines and the app shows them as blocked.
- codex runs with `--sandbox read-only` and the MCP servers from its own
  config. A command the sandbox blocks fails inside codex, which reports no
  list of refusals, so it shows only as a tool line that failed.

## Where a chat runs

A new chat runs on `--workspace`, or `--local` for this computer. With
neither, it runs on `chat.default_workspace`; with that unset, on the first
workspace in `config.yml`; with no workspace at all, on this computer. A
chat keeps where it started: `--chat` with another harness, workspace or
folder is refused rather than quietly moved.

It runs in `~/chats` on that machine (`chat.folder`), created on first use,
so a question with no project does not land in one of your repositories.

The harness is found the way your login shell finds it: the command asks
the account's login shell for its `PATH`, and adds `~/.local/bin`, where the
claude and codex packages install. The message itself never passes through
that shell: it goes to `/bin/sh` on its input, so a fish or zsh login shell
cannot read its quotes differently.

## The local copy

Every message and answer is also kept on your computer, in
`<config>/chats/` (`index.json` for the list, one `<id>.jsonl` per
conversation). The app reads this copy, so opening an old chat needs no
connection. Writes go through a lock and a rename, so the app and a
terminal writing at once lose nothing.

The folder holds private conversations. It carries its own `.gitignore`,
and `devmachine setup git` refuses to commit it.

Deleting a chat removes the local copy only. The harness's transcript on
the workspace stays.

## Asking about a session

`devmachine chat ask` answers a question about a coding session without
typing into it or interrupting it. It runs where the session's transcript
is: on the workspace for a workspace session, here for a session on this
computer.

It first finds the session's folder (from tmux, or `--folder`), the harness
running there, and its newest transcript: the same signals the Context tab
uses.

- **claude: a fork.** It runs `claude --resume <session> --fork-session`.
  The fork starts with the whole session in context and gets a new id; the
  session's own transcript is not written to. Follow-up questions continue
  the fork.
- **codex: a transcript excerpt.** codex has no fork for a command run, so
  the CLI reads the session's rollout file and sends a new read-only codex
  an excerpt: the last six turns, plus the lines that share a word with the
  question, up to 24 KB. The record's `excerpts` says how many pieces went
  in. Follow-ups continue that new codex thread.

Other harnesses get `harness-unsupported`. An ask is a chat with
`kind: "ask"`, left out of `chat list` until `chat continue` makes it a
chat of its own.
