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
- Every other tool follows the workspace's own Claude settings and its
  permission allow list. With `--mcp` that includes its MCP servers: on
  2026-10-16 an MCP tool that only reads (Linear's list_teams) ran in a
  chat, and so did a Bash command that only reads.
- A tool that would need your approval is refused, since a chat has nobody
  to ask. claude lists those at the end of its answer; the chat passes them
  on as `denied` lines and the app shows them as blocked.
- codex runs with `--sandbox read-only`, and with `--mcp` the MCP servers
  from its own config. A command the sandbox blocks fails inside codex,
  which reports no list of refusals, so it shows only as a tool line that
  failed.

## Warm sessions

`chat send` starts the harness for every message, and the start-up is most
of the wait. `chat session` (and `chat ask --serve`) start it once, before
the first message, and keep it running: claude reads one message after
another on its stdin (`--input-format stream-json`) and keeps the same
session id and context. On a workspace the harness runs over one ssh
session that stays open.

Measured on 2026-10-16 (claude 2.1.301 on this computer, 2.1.300 on a
workspace), time from sending "Reply with exactly: …" to the first word of
the answer:

| | This computer | Workspace |
| --- | --- | --- |
| `chat send`, one process per message | 4.6 s | 3.2 s |
| `chat send --mcp` | 6.7 s | 6.9 s |
| `chat session`, first message | 2.0 s | 2.3 s |
| `chat session`, next message | 1.2 s | 2.1 s |

`ready` itself comes back in well under a second; the harness goes on
starting while the person types. On a workspace session, `chat ask
--serve` answered the first question in 4.1 s and the next in 2.0 s,
against 8.6 s for a one-off ask before the changes on this page.

A session closes after 10 minutes with no message, or when its stdin ends,
so nothing is left running for a window nobody looks at. codex has no mode
that keeps reading messages, so its sessions still run `codex exec` per
message.

A stop in the middle of an answer cannot be sent to claude: the process is
stopped and started again with `--resume`, which keeps the conversation.

## MCP servers are off unless you ask

Most of the time a harness takes to answer goes into starting up, and most
of the start-up is the account's MCP servers, hooks and plugins. Measured on
a workspace with 14 MCP servers (claude 2.1.300), "ok" took:

| How claude ran | Time |
| --- | --- |
| `claude -p`, everything on | 12.5 s |
| `--strict-mcp-config --setting-sources project` | 5.5 s |
| the same, on haiku with no tools (an ask) | 3.6 s |
| a reused ssh connection running `true` | 0.2 s |

So a chat runs without them: claude gets `--strict-mcp-config
--setting-sources project`, which starts no MCP server and reads no user
settings, so no user hooks or plugins run. The login is not a setting and
still works.

`--mcp` on `chat send` or `chat session` turns them on, for a chat that
needs a tool from an MCP server. The choice is kept on the chat (`"mcp":
true`): later messages use it until `--mcp` or `--mcp=false` changes it.
`devmachine config set chat.mcp true` makes it the default for new chats;
the app's Settings → Chats switch writes the same key.

codex has no flag that skips only its MCP servers: `-c mcp_servers={}` did
not stop them. Without `--mcp` it runs with `--ignore-user-config`, which
skips all of `~/.codex/config.toml` and keeps the login. A chat then uses
codex's default model, not the one your config chooses; `--mcp` brings
your config back, with its MCP servers.

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

Then, on every question, it reads that transcript as it is now (a claude
`.jsonl`, or a codex rollout) and turns it into plain text: what you
asked, what the agent answered, and one line per tool it used, with long
tool output cut short and its private reasoning left out. From that it
keeps the last six turns, plus the lines that share a word with the
question, up to about 28 KB. The record's `excerpts` says how many pieces
went in.

A transcript can be tens of megabytes, so it is not copied whole. The
machine sends the last megabyte, plus the older lines that hold a word of
the question, which `grep` finds there. When the caller names the harness,
as the app does, finding the session and reading its transcript take one
trip over the connection. On a workspace with a 22 MB transcript this part
went from 1.5 s to 0.55 s.

Before the question it adds the earlier questions and answers of this ask,
the last six or about 8 KB, so a follow-up such as "and the tests?" makes
sense.

That prompt goes on stdin to the session's own harness, set up to answer
fast and touch nothing:

- claude: `claude -p --strict-mcp-config --setting-sources project
  --disable-slash-commands --tools "" --no-session-persistence --model
  haiku`. No MCP server starts and no tool can run, and nothing is saved.
- codex: `codex exec --ephemeral --ignore-user-config --sandbox read-only`.
  It keeps its login but skips `config.toml`, so none of its MCP servers
  start, and it saves no rollout.

Each question also finds the session again. A claude session can move to
a new id, after `/clear` for example, and the next question then reads the
newest transcript, not the one the ask started with. `source_session_id`
follows it.

### Why not a fork

The first version asked claude to fork the session (`claude --resume
<session> --fork-session`) and resumed that fork for follow-ups. That had
two problems:

- A fork re-sends the whole session to the model on every question. On a
  17 MB session that was 639,041 tokens of cache writes for one question:
  slow, and a big bite of your usage limit.
- A fork knows only what the session had done when it was made. Anything
  the session did after the first question was invisible to every later
  one.

Reading the transcript again on each question fixes both. The answer
comes from the part of the session that matters for the question, and it
is always the session as it is now. A lean run answers a short question in
about 2 to 3 seconds.

Other harnesses get `harness-unsupported`. An ask is kept like a chat,
with `kind: "ask"`, and `chat list` leaves it out unless `--all`. It stays
an ask: it goes on only with `chat ask --chat`, and `chat send` refuses it,
because it has no harness session to send to.
