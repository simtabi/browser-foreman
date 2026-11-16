# AI: tags and duplicates

A model can suggest tags for bookmarks, and judge whether bookmarks that look alike are the same page. It is off by
default. Nothing about your bookmarks leaves this computer until you choose a provider and ask for a job.

## Who answers

| `ai.provider` | Who answers a job | What leaves this computer |
|---|---|---|
| `none` (the default) | an assistant connected over [MCP](mcp.md), with its own model | whatever that assistant's own model receives |
| `anthropic` | Anthropic's Messages API, the moment the job is made | the job's titles, addresses, folders and page descriptions |
| `ollama` | a model in a local [Ollama](https://ollama.com), the moment the job is made | nothing, when Ollama runs on this computer |

| Key | Variable | Default | What it does |
|---|---|---|---|
| `ai.provider` | `BROFO_AI_PROVIDER` | `none` | `none`, `anthropic` or `ollama` |
| `ai.model` | `BROFO_AI_MODEL` | `claude-opus-5-5` for `anthropic` | the model; with `ollama`, required: one you have pulled (`ollama list`) |
| `ai.effort` | — | `low` | with `anthropic`: `low`, `medium` or `high`; how much the model thinks before answering |
| `ai.ollama_url` | — | `http://127.0.0.1:11434` | where Ollama listens |
| — | `ANTHROPIC_API_KEY` | none | the key, in `.env` or the environment only; without it, a profile from `ant auth login` is used |

With `anthropic`, every answer is held to a JSON schema, so it comes back as data rather than prose. A request the
model declines is reported as failed, and Claude Opus 5.5 is asked with refusal fallbacks on (the API retries a declined
request on another model). Tagging is short classification work, so `ai.effort` starts at `low`.

## Making jobs

Jobs live in the running server (`browser-foreman serve`), so it must be running:

```bash
browser-foreman ai tag main --folder "Bookmarks Bar/Dev"          # suggest tags, change nothing
browser-foreman ai tag main --folder "Bookmarks Bar/Dev" --apply  # and add them
browser-foreman ai dedupe main --apply                            # judge look-alikes, trash the copies
browser-foreman ai jobs                                           # where each job stands
```

- **`ai tag`** takes the bookmarks without tags (`--retag` includes the others), up to `--limit` (200; at most 1000),
  in jobs of 25. Each bookmark gets one to three short lower-case tags, preferring tags already in use.
- **`ai dedupe`** sends only the groups [Find duplicates](workspaces.md) is unsure of: ones matched by a shared
  canonical page or a similar title. Groups that share an address, or where one leads to the other, are certain
  already and are not sent. Jobs hold 10 groups each.
- **Without `--apply`** the answers are shown and nothing changes. With it, each job is one history entry: tags are
  added (none is removed), and copies judged the same page go to the Trash with the reason
  `duplicate of <id> (judged the same page by a model)`. Undo takes either back.

With a provider, the command waits for the answers (up to ten minutes) and prints them. With `none`, the jobs wait
for an assistant.

## Answering jobs from an assistant

With writes on (see [MCP](mcp.md)), an assistant has three tools and a prompt for the queue:

| | |
|---|---|
| `next_ai_jobs` | takes up to five waiting jobs for 15 minutes, with the JSON each is answered with |
| `submit_ai_results` | answers one job |
| `run_ai_jobs` | answers waiting jobs with the assistant's own model through MCP sampling, when the client offers it |
| prompt `process-queue` | asks the assistant to work through the queue until it is empty |

Under a [folder scope](mcp.md#write-scopes), an assistant may answer a tag job only for a folder inside its scope,
and no dedupe job, since that can touch the whole workspace.

## What an answer may do

Every answer, from any provider or assistant, is checked against its job before anything is written:

- tags only for the job's own bookmarks, at most three each, each up to 40 characters and without commas;
- one verdict per group, naming only that group's members, and never the bookmark kept.

Anything else in an answer is dropped. Bookmarks are sent to the model marked as data, with the instruction never to
follow text written in them, since a title or page can hold text meant to steer a model; the check above is what
holds if one does.

## Through the API

All owner only:

| | |
|---|---|
| `POST /api/v1/ai/jobs` | makes jobs: `{kind: tag \| dedupe, workspace, folder?, apply?, limit?, retag?}` |
| `GET /api/v1/ai/jobs` | the newest 100 jobs, with their state and checked answers |
| `POST /api/v1/ai/jobs/next` | takes waiting jobs: `{max?, by?}` |
| `POST /api/v1/ai/jobs/{id}/results` | answers one: `{results: {tags?, verdicts?}, by?}` |

Events: `ai.jobs.created`, `ai.job.done` and `ai.job.failed`. Jobs are kept in memory, the newest 500; a restart
forgets them, and making them again takes a moment.

---
[← Docs index](../../README.md#documentation)
