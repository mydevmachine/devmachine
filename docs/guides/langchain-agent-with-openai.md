---
description: "Build a small LangChain agent with two tools in a workspace, with its OpenAI key delivered by devmachine."
category: Agents
level: Beginner
needs:
  - "A Debian or Ubuntu VPS, or a local VM"
  - "An OpenAI API key"
related:
  - logins-and-secrets.md
  - agno-agentos-with-control-plane.md
  - a-local-vm-with-lima.md
---
# A simple agent with LangChain and OpenAI

Write a small Python agent with [LangChain](https://docs.langchain.com/oss/python/langchain/overview)
in workspace `acme`. It has two tools, one that reads the server's clock
and one that counts words, and it answers a question you give it on the
command line.

The OpenAI key never goes in a command, a script or the chat with your
agent: `devmachine secrets` stores it on your computer and writes it into
the project's `.env` file in the workspace.

**You need:** a Debian or Ubuntu VPS, or a [local VM](a-local-vm-with-lima.md)
(this guide needs no domain), and an OpenAI API key from
[platform.openai.com](https://platform.openai.com/api-keys).

## Before you start: machine, skills, workspace

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
devmachine workspaces new acme
devmachine sync
```

Already have a machine? Skip `setup`. Already have the workspace? Skip the
last two. See [getting started](../getting-started.md) for what each command
does.

## By hand

### 1. Make the Python project

Inside the workspace (`devmachine ssh acme`):

```
mise use -g uv@latest
uv init agent && cd agent
uv add langchain langchain-openai python-dotenv
echo .env >> .gitignore
```

[uv](https://docs.astral.sh/uv/) installs Python packages, and Python
itself if the machine lacks the right version. `mise`, from the `mise`
package every new workspace gets, installs it without root. `uv init` makes
a git repository, so the last line keeps the key file out of it.

### 2. Deliver the OpenAI key

On your computer:

```
devmachine secrets set OPENAI_API_KEY --workspace acme --env-file agent/.env --push
```

```
value for OPENAI_API_KEY:
stored acme/OPENAI_API_KEY
delivered acme/OPENAI_API_KEY -> agent/.env
```

Paste your key at the prompt. It does not echo, so it never reaches your
shell history. The value is kept in your OS keychain, and `--push` writes it
at once to `/home/acme/agent/.env`, a file only `acme` can read:

```
OPENAI_API_KEY='<your key>'
```

The path after `--env-file` is relative to the workspace's home. Any other
lines in that file stay as they were. To change the key later, run the same
command again. See [credentials: your app's own
secrets](../concepts/credentials.md#your-apps-own-secrets).

### 3. Write the agent

Inside the workspace, in `~/agent`:

```
cat > agent.py <<'EOF'
import sys
from datetime import datetime

from dotenv import load_dotenv
from langchain.agents import create_agent
from langchain.tools import tool

load_dotenv()


@tool
def get_time() -> str:
    """Return the current date and time on this server."""
    return datetime.now().isoformat(timespec="seconds")


@tool
def word_count(text: str) -> int:
    """Count the words in a piece of text."""
    return len(text.split())


agent = create_agent(
    model="openai:gpt-6-luna",
    tools=[get_time, word_count],
    system_prompt="You are a helpful assistant. Use your tools when they help.",
)

if __name__ == "__main__":
    question = " ".join(sys.argv[1:]) or input("Question: ")
    result = agent.invoke({"messages": [{"role": "user", "content": question}]})
    print(result["messages"][-1].content)
EOF
```

`load_dotenv()` reads `.env` and sets `OPENAI_API_KEY`, which LangChain's
OpenAI model reads by itself. The docstring of each tool is what the model
reads to decide when to call it. `gpt-6-luna` is OpenAI's low-cost model;
any name from [OpenAI's model list](https://developers.openai.com/api/docs/models)
works after `openai:`.

### 4. Ask it something

Inside the workspace, in `~/agent`:

```
uv run agent.py "What time is it on the server, and how many words are in 'hello big world'?"
```

The model calls both tools, then writes one answer with the server's time
and the number 3. With no question after `agent.py`, it asks you for one.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
In my devmachine workspace acme, make a uv project ~/agent with langchain,
langchain-openai and python-dotenv, and write agent.py: a LangChain
create_agent agent on openai:gpt-6-luna with two tools, get_time and
word_count, that answers the question given on the command line. Read
OPENAI_API_KEY from agent/.env. Do not ask me for the key: tell me the
devmachine secrets command to run myself.
```

The agent installs uv, makes the project and writes `agent.py` over SSH.
The key is yours to give: run the `devmachine secrets set ... --push`
command from step 2 yourself, and paste the key at its prompt, never into
the chat. Then the agent can run `uv run agent.py` to test it.

**Check it:** inside the workspace, `cd ~/agent && uv run agent.py "How many
words are in 'hello big world'?"` prints an answer that says 3.

## Troubleshooting

**`OpenAIAuthenticationError: Error code: 401 ... 'code': 'invalid_api_key'`**
means the code works: the packages are installed, the tools load, the key
was read and the request reached OpenAI, which refused the key. The key is
wrong, not the code. The message shows only its first and last letters,
such as `Incorrect API key provided: sk-test-**********-key`. Copy a fresh
key from OpenAI and run step 2 again.

**`openai.OpenAIError: Missing credentials. Please pass an api_key ...`**
means no key was found. Check that `~/agent/.env` exists and that you run
the agent from `~/agent`. On your computer, `devmachine secrets list
--workspace acme` shows where the key goes:

```
acme/OPENAI_API_KEY -> agent/.env
```

To test the tools without the model or a key, inside `~/agent`:

```
uv run python -c 'from agent import get_time, word_count; print(get_time.invoke({})); print(word_count.invoke({"text": "hello big world"}))'
```

It prints the server's time, then `3`.

## Next steps

- Put the agent behind a small web API and give it a domain:
  [FastAPI with TLS](fastapi-with-tls.md) shows the app, then
  `devmachine expose add` publishes it.
- Give it memory between questions: pass a `checkpointer` to
  `create_agent` and the same `thread_id` on each call — see LangChain's
  [short-term memory](https://docs.langchain.com/oss/python/langchain/short-term-memory).
- Add more tools: any Python function with type hints and a docstring,
  marked `@tool`.

Source: [LangChain — Quickstart](https://docs.langchain.com/oss/python/langchain/quickstart)
