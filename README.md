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

macOS, through Homebrew:

```bash
brew install aramponi/tap/yakanban
```

Anywhere Go is installed:

```bash
go install github.com/aramponi/yakanban/cmd/yakanban@latest
```

Or grab a static binary for macOS, Linux or Windows from the
[releases](https://github.com/aramponi/yakanban/releases). The Homebrew route
is macOS-only: it ships as a cask, which is a macOS mechanism.

The Homebrew command is deliberately the fully qualified one: it trusts that
single cask on the spot. Homebrew 6 requires third-party taps to be trusted,
so the short form needs one extra step:

```bash
brew tap aramponi/tap
brew trust --cask aramponi/tap/yakanban
brew install yakanban
```

### As a GitHub CLI extension

The same binary answers to `gh yakanban ...` when it is installed under the
name `gh-yakanban` — help output adapts automatically:

```bash
make gh-extension     # builds and installs into gh's extension directory
gh yakanban board
```

This is a local build, not a release artifact. `gh extension install` resolves
a *repository* whose name begins with `gh-`, which this one does not, so a
published `gh-yakanban` binary could never be installed the documented way.
Shipping one would have been dead weight in every release.

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
| `skill install` | Install the bundled agent skills |

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

### Branching models

`yakanban init` asks which model the repository follows, and writes the answer
to the descriptor. `--branching <model>` skips the question; a piped or CI run
never prompts.

| Model | base → integration | branch |
|---|---|---|
| `trunk-based` | `main` → `main` | `{{.ID}}-{{.Slug}}` |
| `github-flow` | `main` → `main` | `{{.ID}}-{{.Slug}}` |
| `git-flow` | `develop` → `develop` | `{{.Type}}/{{.ID}}-{{.Slug}}` |
| `gitlab-flow` | `main` → `main` | `{{.ID}}-{{.Slug}}` |
| `oneflow` | `main` → `main` | `{{.Type}}/{{.ID}}-{{.Slug}}` |

`model` picks a preset and every key beside it is an override, so `custom`
is simply "no preset":

```yaml
branching:
  model: git-flow
  base: develop           # where work starts
  integration: develop    # where it merges back
  templates:
    branch: "{{.Type}}/{{.ID}}-{{.Slug}}"
    worktree: "../{{.Repo}}-task-{{.ID}}"
  types:
    default: feature
    match:                # first match wins
      - priority: critical
        type: hotfix
        base: main        # a hotfix branches off production
      - tag: bug
        type: fix
```

Templates can use `.ID`, `.Slug`, `.Title`, `.Priority`, `.Class`, `.Type`,
`.Agent`, `.Board` and `.Repo`. `yakanban config` prints the resolved model and
its rules; `yakanban branch ID --dry-run --json` prints the names a given task
would get without creating anything.

Release branches and git flow's hotfix back-merge are deliberately **not**
automated: that is release engineering, and a silently skipped back-merge
loses a fix. yakanban says the back-merge is yours to do and stops there.

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

### Changing the columns

Columns belong to the tracker, not to yakanban. Add, rename, reorder or remove
one in the GitHub project's own UI and yakanban follows on the next command —
no re-init, no edit required. The ordering the UI shows is the ordering
`--next` and `--prev` walk.

```bash
yakanban board --refresh    # see it now rather than after the 1-minute cache
```

The descriptor only adds what GitHub has nowhere to store: which column is
terminal (entering it stamps `Completed`), which is the intake column (leaving
it stamps `Started`), which requires a claim, and WIP limits. A column added in
the UI starts with none of those — give it some by adding it to `statuses:`:

```yaml
statuses:
  - name: Backlog
    initial: true
  - name: Todo
  - name: In Progress
    require_claim: true
    wip_limit: 3
  - name: Review          # added in the web UI, then described here
    require_claim: true
  - name: Done
    terminal: true
defaults:
  review: Review          # where `yakanban handoff` parks work
```

Names are matched case-insensitively and ignoring separators, so a column
renamed to `in progress` keeps the semantics written for `In Progress`.

**Do not use `yakanban init --force` to pick up a new column.** It rewrites the
descriptor from the defaults and would discard WIP limits, claim rules and
anything else you have written there. Editing `statuses:` is the supported
route; `init --force` is for re-provisioning the backend.

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

Two skills ship inside the binary, so a downloaded release installs them with
no checkout:

- **`yakanban`** — the decision tree, the claim protocol and the exit codes,
  for any task or board work.
- **`yakanban-based-development`** — the autonomous, parallel-safe development
  loop: pick → branch → implement → merge → done, with an explicit handoff
  protocol for anything needing a human.

```bash
yakanban skill install            # into this project, versioned with the code
yakanban skill install --global   # once, for every project on the machine
yakanban skill check              # exits non-zero when an installed skill is stale
yakanban skill update             # refresh what is installed
```

Claude Code, Codex, Cursor, Gemini CLI, Antigravity, Hermes, Pi and OpenClaw
are supported. Run `skill install` in a terminal and it shows every one of
them, with the detected ones already ticked and the evidence beside each:

```
Install the yakanban skills for:

  [x] 1) claude       found: ~/.claude
  [x] 2) codex        found: codex on PATH
  [ ] 3) cursor       not detected
  [x] 4) gemini       found: ~/.gemini
  [ ] 5) antigravity  not detected
  [ ] 6) hermes       not detected
  [x] 7) pi           found: pi on PATH
  [ ] 8) openclaw     not detected

Toggle with a number, Enter to install, q to cancel:
```

Detection proposes; you decide. `--agent` gives the same answer up front and
skips the menu, `--path` writes anywhere, and a piped or CI run installs for
everything detected without asking.

A skill file you have edited is never overwritten without `--force`: each
installed file carries a version marker and a hash of the text yakanban wrote,
so "stale" and "you changed this" are told apart.

Two agents need a word of explanation:

- **Hermes** is the exception to "installed means active": it does not load
  project skills until `hermes skills trust` runs, so `install` and `check`
  say so rather than reporting a file nothing will read.
- **Antigravity** is an IDE, so there is no executable to detect it by, and it
  reads the same `.agents/skills` directory as Codex. Selecting both writes
  that file once, not twice. Its user-level skills live under
  `~/.gemini/config/skills`, inside Gemini CLI's directory, so each is
  detected by its own path rather than by the root they share.

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
make check     # fmt, vet, lint, test
```

`make check` runs golangci-lint, pinned to the same version as CI:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0
```

## License

MIT — see [LICENSE](LICENSE).
