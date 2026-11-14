# Sorting into folders

*Sort into folders…* files a workspace's bookmarks into folders by rules you write: the step that turns a long
"Other bookmarks" pile into a tree. The rules are a JSON file in your data folder, `sort-rules.json`, because they
name your own folders and sites. The program only knows how to follow them.

## What happens to each bookmark

In this order, the first that applies wins:

1. In a **protected** folder: it stays exactly where it is.
2. **Dead**: it goes to `review.dead` (confirmed-dead links only, unless `dead_unconfirmed` is true).
3. Matches a protected folder (its `match` pattern sees "host title", or the host is one that folder already holds,
   apart from `generic_hosts`): it moves there.
4. Listed in **`manual`** (address → folder) and outside every folder: it goes there. Once you file it yourself, a
   later sort leaves it alone.
5. Loose on the **bar** (`bar`): it moves only when its topic starts with `bar_topic_prefix` and `bar_topic_folders`
   names a folder for it; otherwise it stays.
6. Already in a folder you made: it **stays**.
7. Loose, outside every folder:
   - not a web page, a local or staging address, a saved Google search or a sign-out page goes to the matching
     `review` folder;
   - otherwise its **topic** is the first `topics` rule it matches, or `archive.topic` when it was added in or
     before `archive.year`;
   - the topic's folder is `topic_folders[topic]`, or the first `prefix_folders` entry whose topic starts it
     (folder plus the rest of the topic);
   - with no topic it goes to `review.unsorted`.

Then three passes make a second level, and each makes a subfolder only for a group of at least `type_min` (default 4):

| Pass | Where | Groups by |
|---|---|---|
| `splits` | each folder under `under` (prefix) or in `in`, with at least `min` bookmarks | `types`, matched against "host title path"; the rest go to `default`. `aliases` reuses a folder you already have, and `rename` moves the base. |
| `piles` | each folder whose last name contains `leaf_contains`, starts with `leaf_prefix` or is one of `leaf_in`, with at least `min` bookmarks | `groups`, matched against "host path title"; the rest go to `fallback` |
| `groups` | the folders named as keys | the listed groups; the rest stay |

A topic rule's `host` matches the host without `www.`, and `text` matches "title host path" with file extensions
dropped. `repo` matches "repository-name title" for a `github.com` repository, and `profile` matches a `github.com`
address with fewer than two path parts. Every pattern given must match. Patterns are Go regular expressions and
ignore case.

## A small rules file

```json
{
  "bar": "Bookmarks Bar",
  "protected": [{"folder": "Bookmarks Bar/School", "match": "(^|\\.)school\\.example$"}],
  "generic_hosts": ["github.com", "google.com"],
  "review": {"dead": "Bookmarks Bar/_Review/Dead links", "unsorted": "Bookmarks Bar/_Review/Unsorted",
             "not_web": "Bookmarks Bar/_Review/Not web links", "local": "Bookmarks Bar/_Review/Local & staging"},
  "topics": [
    {"host": "^github\\.com$", "repo": "laravel|php", "topic": "Dev/PHP"},
    {"host": "php\\.net$", "topic": "Dev/PHP"},
    {"text": "\\brecipe", "topic": "Home/Cooking"}
  ],
  "topic_folders": {"Home/Cooking": "Bookmarks Bar/Kitchen"},
  "prefix_folders": [{"topic": "Dev/", "folder": "Bookmarks Bar/Development/"}],
  "archive": {"year": 2019, "topic": "Old"}
}
```

## Running it

From the web interface: the workspace menu or the command palette › *Sort into folders…*. It shows where each
bookmark would go, folder by folder, with the rule that sent it there. Untick a folder to leave its bookmarks where
they are. *Move* makes the ticked moves as one history entry, so Undo puts everything back.

Running it again only moves what the rules now place differently. A folder a sort made is not split again. A big
folder you made since is split if the rules say so, and the tick boxes are how you keep it whole. From the command
line:

```bash
browser-foreman sort --workspace main                 # what would move, by folder; nothing changes
browser-foreman sort --workspace main --apply         # make the moves
browser-foreman sort --workspace main --rules ~/my-rules.json
```

The API is `POST /api/v1/workspaces/{id}/sort` (editor) with `{}` for the plan or `{"apply": true}`, and
`"folders": [...]` to limit either to some destinations. It answers 409 while there is no rules file.

## Coming from the Python tool

The Python tool's `build.py` held these rules in its source. Its tables map one to one: `PROTECTED`,
`GENERIC_HOSTS`, `GITHUB_TOPICS`, `DOMAIN_RULES` and `KEYWORD_RULES` become `topics`; `MERGE_TOPIC` and `MINE_MAP`
become `topic_folders`; `TYPE_RULES` becomes `splits`; `QA_TOPICS` and `REPO_KINDS` become `piles`;
`WEBSITE_GROUPS` becomes `groups`; and `manual-folders.json` becomes `manual`. Measured on the 2026-11-10 Chrome
export (6,158 bookmarks after duplicates), the Go rules put 6,141 in the same folder as the Python build. The other
17 differ only because their link status changed between the two runs.

---
[← Docs index](../../README.md#documentation)
