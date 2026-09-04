# yakanban — Yet Another Kanban

A Kanban CLI for teams where **humans and AI agents share the same board**.

Agents and developers drive tickets from the terminal. Everyone else keeps
using the native web UI of the tracker — GitHub Projects today, Jira, Plane or
Linear later. yakanban never becomes the source of truth: the backend does.

```console
$ yakanban board
app board (github)
https://github.com/users/acme/projects/3

COLUMN       TASKS  WIP  BLOCKED  CLAIMED
Backlog      12     —    —        —
Todo         5      —    —        —
In Progress  2      2/3  1        2
Review       1      —    —        —
Done         38     —    —        —

total 58   blocked 1   overdue 2
```

## Why

[kanban-md](https://github.com/antopolskiy/kanban-md) gets the model right —
statuses, priorities, classes of service, claims, dependencies, handoffs — but
its board is a directory of Markdown files, so sharing it with people outside
the repository is awkward. yakanban keeps that model and moves the storage to
a tracker the whole team already has an account for.

- **The backend is the source of truth.** Every write goes straight to it.
  Reads go through a small local TTL cache so agents can poll the board without
  burning API quota.
- **No second UI.** A product owner reorders the GitHub project board in the
  browser; the agent sees it on its next command.
- **Nothing provider-shaped leaks into the model.** Optional fields stay
  optional, and anything backend-specific lives in `Metadata map[string]any`.

## Install

```bash
go install github.com/aramponi/yakanban/cmd/yakanban@latest
```

Or grab a static binary for macOS, Linux or Windows from the
[releases](https://github.com/aramponi/yakanban/releases).

### As a GitHub CLI extension

The same binary answers to `gh yakanban ...` when it is installed under the
name `gh-yakanban` — help output adapts automatically:

```bash
make gh-extension     # builds and installs into gh's extension directory
gh yakanban board
```

Both entry points run the same code; use whichever fits your muscle memory.

## Authentication

yakanban stores no credentials. It uses, in order:

1. `$YAKANBAN_GITHUB_TOKEN`, `$GH_TOKEN` or `$GITHUB_TOKEN`
2. `gh auth token` — the GitHub CLI login you already have

Projects v2 lives behind its own OAuth scope. If you authenticated with `gh`
before ever touching projects, add it once:

```bash
gh auth refresh -s project
```

## Quick start

```bash
# In a git repository whose origin points at GitHub:
yakanban init                      # creates a Project v2, links it, writes .yakanban.yml
yakanban init --project 7          # …or adopts an existing project, untouched

yakanban create "Fix the login redirect" --priority high --tags bug
yakanban list --compact --status todo
yakanban move 42 in-progress --claim $(yakanban agent-name)
yakanban edit 42 -a "Root cause: stale session cookie." -t
yakanban move 42 done
```

`init` writes a committed `.yakanban.yml` next to your code and adds
`.yakanban/` (the read cache) to `.gitignore`.

## Commands

| Command | What it does |
|---|---|
| `init` | Provision the board and write the descriptor |
| `list` | List and filter tasks |
| `show ID` | Full detail of one task |
| `create TITLE` | Open an issue and put it on the board |
| `edit ID` | Change fields, claim, block, append to the body |
| `move ID STATUS` | Change column (`--next` / `--prev` also work) |
| `pick --claim AGENT` | Claim the next available task, atomically |
| `branch ID` | Create a branch and attach it to the ticket |
| `handoff ID --claim AGENT` | Park a task with a note for whoever picks it up |
| `delete ID --yes` | Close the issue and archive its board item |
| `board` | Column counts, WIP pressure, blocked and overdue |
| `sync` | Drop the read cache and refetch |
| `config` | Show the resolved configuration |
| `agent-name` | Generate a claim identifier |

Every command accepts `--json`, `--compact` (one line per record, cheap for
agents), `--table`, `--no-color`, `--refresh` and `--no-cache`.

### Filtering

```bash
yakanban list --status todo,in-progress --priority high,critical
yakanban list --tag bug --assignee alice --sort priority -r
yakanban list --unclaimed --not-blocked --status todo   # ready to pick up
yakanban list --unblocked                               # dependencies all done
yakanban list --search "login" --limit 10
```

## Agents

Claims are soft, expiring locks (default one hour) that keep two agents from
grabbing the same ticket. They live in the project as two ordinary fields,
`Claim` and `Claim Expires`, so a human can see who holds what.

```bash
AGENT=$(yakanban agent-name)                       # e.g. frost-maple-07

yakanban pick --claim "$AGENT" --status todo --move in-progress
yakanban edit 42 -a "Tests green, opening a PR." -t --claim "$AGENT"
yakanban handoff 42 --claim "$AGENT" --note "Ready to merge: task/42-login" -t --release
yakanban edit 42 --release && yakanban move 42 done
```

`pick` takes the highest-priority task that is unclaimed, unblocked and whose
dependencies are done. Prefer it over list → edit → move: a task another agent
takes in between is detected and skipped, which is what makes several agents
safe on one board.

A write with `--claim` fails with exit code 5 when another agent holds an
unexpired claim; `--force` overrides it. A plain human edit is never blocked.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | unexpected error |
| 2 | bad input or missing configuration |
| 3 | task not found |
| 4 | authentication or scope problem |
| 5 | the task is claimed by another agent |
| 6 | the provider cannot express what you asked for |

## Branches

`yakanban branch` creates a branch on the backend and attaches it to the
ticket — GitHub's linked branches, shown in an issue's Development section. The
work becomes visible to people who have never heard of yakanban, and the pull
request closes the issue on merge without a `closes #N` keyword.

```bash
BRANCH=$(yakanban branch 42 --claim "$AGENT")
git worktree add ../app-task-42 -b "$BRANCH" origin/main
```

The name is decided locally, from a template, and handed to the backend — never
read back from it. That is what lets an agent create its local branch straight
away, from a commit it already has, with no fetch in between.

```yaml
branching:
  templates:
    branch: "{{.ID}}-{{.Slug}}"          # matches what GitHub generates itself
    worktree: "../{{.Repo}}-task-{{.ID}}"
```

Templates can use `.ID`, `.Slug`, `.Title`, `.Priority`, `.Class`, `.Agent`,
`.Board` and `.Repo`. `--dry-run --json` prints the names that would be used
without creating anything.

The base commit defaults to the upstream of the current branch and is always
resolved through the **remote**: a commit you have not pushed does not exist
for the backend, and the API error for that says nothing useful.

Branches are an optional capability. A provider that has no such notion reports
it rather than silently doing nothing.

## Configuration

`.yakanban.yml` is committed. It holds no secrets — only the vocabulary and
which backend to talk to:

```yaml
version: 1
provider: github
board:
  name: app
statuses:
  - name: Backlog
    initial: true          # leaving this column stamps Started
  - name: Todo
  - name: In Progress
    require_claim: true    # agents must pass --claim to move here
    wip_limit: 3
  - name: Review
    require_claim: true
  - name: Done
    terminal: true         # entering this column stamps Completed
priorities: [low, medium, high, critical]
classes:
  - name: expedite
    wip_limit: 1
    bypass_column_wip: true
  - name: standard
defaults:
  status: Backlog
  priority: medium
  class: standard
claim_timeout: 1h
cache:
  enabled: true
  ttl: 1m0s
providers:
  github:
    owner: acme
    repo: app
    project_number: 3
```

Column names and order come from the backend at runtime — rename a column in
the GitHub UI and yakanban follows. The descriptor only adds the semantics
GitHub has nowhere to put: which column is terminal, which requires a claim,
WIP limits.

## How it maps onto GitHub

Issues carry the content, the project carries the workflow. See
[docs/github-mapping.md](docs/github-mapping.md) for the full table.

| yakanban | GitHub |
|---|---|
| task ID | issue number |
| title, body, tags, assignees | issue title, body, labels, assignees |
| status | project `Status` column |
| priority, class | project single-select fields |
| due, started, completed | project date fields |
| estimate, blocked, parent, depends on | project text fields |
| claim | project `Claim` + `Claim Expires` fields |

## Claude Code / agent skill

Two ready-made skills live in `skills/`; copy either to `~/.claude/skills/`
(or your team's skills directory):

- **`yakanban`** — the decision tree, the claim protocol and the exit codes,
  for any task or board work.
- **`yakanban-based-development`** — the autonomous, parallel-safe development
  loop: pick → worktree → implement → merge → done, with an explicit handoff
  protocol for anything needing a human.

## Other backends

The domain and the CLI know nothing about GitHub. A backend is one package
implementing `core.Provider` plus one line in the registry — see
[docs/architecture.md](docs/architecture.md#adding-a-provider). Jira, Plane and
Linear are the intended next ones.

## Roadmap

The board runs on yakanban itself:
[github.com/users/aramponi/projects/2](https://github.com/users/aramponi/projects/2).

## Development

```bash
make test      # go test ./...
make build     # ./bin/yakanban
make check     # fmt, vet, test
```

## License

MIT — see [LICENSE](LICENSE).
