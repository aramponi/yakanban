# yakanban — Yet Another Kanban

<!-- site:blurb -->
Let your AI agents work the board your team already uses. No migration, no
second UI, nothing for your product owner to learn.

Your coding agent just finished a task. Nobody else knows: no ticket moved, no
claim was taken, and the second agent you started in another terminal is about
to pick up the same bug.

yakanban is one CLI that puts agents **and** developers on the board you
already have — a GitHub Project or a GitLab board. Claims stop two agents
taking the same ticket. One uniform command shape means every agent drives a
ticket the same way. Everyone else keeps working in the tracker's own web UI,
on that same board, and simply watches the work appear.

```console
$ yakanban board
yakanban (github)
https://github.com/users/aramponi/projects/2

COLUMN       TASKS  WIP  BLOCKED  CLAIMED
Backlog      0      —    —        —
Todo         0      —    —        —
In Progress  1      —    —        1
Review       0      —    —        —
Done         15     —    —        —

total 16   blocked 0   overdue 0
priorities medium 11  high 5
```

That is this project's own board, and it is
[public](https://github.com/users/aramponi/projects/2): yakanban is built with
yakanban. The row in `In Progress` is an agent holding a claim, right now.

## What an agent actually does

```bash
AGENT=$(yakanban agent-name)                  # e.g. frost-maple-07

yakanban pick --claim "$AGENT" --status todo --move in-progress
BRANCH=$(yakanban branch 42 --claim "$AGENT")
yakanban edit 42 -a "Tests green, opening a PR." -t --claim "$AGENT"
yakanban move 42 done
```

Four commands, and in the browser your team sees: the ticket in **In Progress**
with `frost-maple-07` in the Claim column, a branch linked in the issue's
Development section, a timestamped progress note as an issue comment, and
finally the card in **Done** with a completion date.

Nobody had to install anything to see that.

## Why

Coding agents are good at the work and bad at saying what they are doing. The
board is where a team already looks for that answer, so the fix is not another
tracker — it is giving agents the one that exists.

Three commitments follow from that, and they are what yakanban is:

- **You keep your board.** yakanban is a client, not a store. Setup adopts the
  project your team already uses: the columns are taken as found, nothing is
  renamed or deleted, and old issues are not bulk-imported. The greenfield case
  is the same command with nothing to adopt.
- **There is no second UI.** A product owner reorders the project board in the
  browser; the agent sees it on its next command. Every write goes straight to
  the tracker, and reads pass through a one-minute cache so an agent can poll
  without spending someone's API quota.
- **Agents and humans speak one vocabulary.** Fifteen commands, the same global
  flags on every one of them, and exit codes a script can branch on. The claim
  protocol is not a convention each agent reinvents — it ships with the tool,
  as a skill file compiled into the binary.

The model — statuses, priorities, classes of service, claims, dependencies,
handoffs — is borrowed from [kanban-md](https://github.com/antopolskiy/kanban-md),
which gets it right. Its board is a directory of Markdown files, which is
awkward to share with anyone outside the repository. yakanban keeps the model
and moves the storage to a tracker the whole team already has an account for.

That trade has a price worth saying out loud: without a network you have no
board. Offline reads were traded for never being wrong about who owns what.

## Install

macOS, through Homebrew:

<!-- site:install name="Homebrew" -->
```bash
brew install aramponi/tap/yakanban
```

Windows, through Scoop:

<!-- site:install name="Scoop" -->
```powershell
scoop bucket add aramponi https://github.com/aramponi/scoop-bucket
scoop install yakanban
```

Anywhere Go is installed:

<!-- site:install name="go install" -->
```bash
go install github.com/aramponi/yakanban/cmd/yakanban@latest
```

On macOS or Linux, without Go or Homebrew:

<!-- site:install name="Binary" -->
```bash
VERSION=$(curl -fsSL https://api.github.com/repos/aramponi/yakanban/releases/latest | grep -m1 '"tag_name"' | cut -d'"' -f4)
OS=$(uname -s | tr '[:upper:]' '[:lower:]'); ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
curl -fsSL "https://github.com/aramponi/yakanban/releases/download/$VERSION/yakanban_${VERSION#v}_${OS}_${ARCH}.tar.gz" | tar xz yakanban
```

Or grab a static binary from the
[releases](https://github.com/aramponi/yakanban/releases). Releases are signed
and carry build provenance — see
[verifying a release](docs/verifying-releases.md), which also covers the
trusted Homebrew tap and the `gh yakanban` extension build.

## Authentication

yakanban stores no credentials. For GitHub it uses, in order:

1. `$YAKANBAN_GITHUB_TOKEN`, `$GH_TOKEN` or `$GITHUB_TOKEN`
2. `gh auth token` — the GitHub CLI login you already have

Projects v2 lives behind its own OAuth scope. If you authenticated with `gh`
before ever touching projects, add it once:

```bash
gh auth refresh -s project
```

For GitLab, it uses `$GITLAB_TOKEN`, then the existing host-specific `glab`
login (`glab auth login --hostname gitlab.com`). The HTTP client uses Bearer
authentication for both access tokens and OAuth logins.

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

Already have a board, or years of issues? Neither is a problem, and neither is
migrated behind your back: see
[adopting a board that already exists](docs/adopting-a-board.md), which covers
GitLab setup too.

## Commands

<!-- site:table id="commands" -->

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
agents), `--table`, `--no-color`, `--refresh` and `--no-cache`. There is no
command that prints JSON and another that does not.

### Filtering

```bash
yakanban list --status todo,in-progress --priority high,critical
yakanban list --tag bug --assignee alice --sort priority -r
yakanban list --unclaimed --not-blocked --status todo   # ready to pick up
yakanban list --unblocked                               # dependencies all done
yakanban list --search "login" --limit 10
```

### Exit codes

<!-- site:table id="exit-codes" -->

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | unexpected error |
| 2 | bad input or missing configuration |
| 3 | task not found |
| 4 | authentication or scope problem |
| 5 | the task is claimed by another agent |
| 6 | the provider cannot express what you asked for |

Code 5 is not an error: another agent got there first, and the correct response
is to pick something else.

## Agents

Claims are soft, expiring locks (default one hour) that keep two agents from
grabbing the same ticket. They live in the project as two ordinary fields,
`Claim` and `Claim Expires`, so a human can open the board in the browser and
see who holds what, and until when.

```bash
AGENT=$(yakanban agent-name)                       # e.g. frost-maple-07

yakanban pick --claim "$AGENT" --status todo --move in-progress
yakanban edit 42 -a "Tests green, opening a PR." -t --claim "$AGENT"
yakanban handoff 42 --claim "$AGENT" --note "Ready to merge: task/42-login" -t --release
yakanban edit 42 --release && yakanban move 42 done
```

`pick` takes the highest-priority task that is unclaimed, unblocked and whose
dependencies are done, then reads the claim back before returning it. That read
is why several agents on one board is safe: list-then-claim checks nothing,
because the answer is stale by the time it arrives.

A write with `--claim` fails with exit code 5 when another agent holds an
unexpired claim; `--force` overrides it. A plain human edit is never blocked.
See [working with agents](docs/agents.md) for the full protocol.

### The instructions ship with the binary

The hard part of handing a board to an agent is not the API, it is telling it
when to claim, when to park a task and when to stop and ask. Two skills carry
that, compiled into the binary, so a downloaded release installs them with no
checkout:

- **`yakanban`** — the decision tree, the claim protocol and the exit codes.
- **`yakanban-based-development`** — the autonomous, parallel-safe loop: pick,
  branch, implement, merge, done, with an explicit handoff protocol for
  anything needing a human.

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
everything detected without asking. A skill file you have edited is never
overwritten without `--force`: each installed file carries a version marker and
a hash of the text yakanban wrote, so "stale" and "you changed this" are told
apart. [Two agents need a word of explanation.](docs/agents.md#installing-the-skills)

## Branches

`yakanban branch` creates a branch on the backend and attaches it to the
ticket — GitHub's linked branches, shown in an issue's Development section. The
work becomes visible to people who have never heard of yakanban, and the pull
request closes the issue on merge without a `closes #N` keyword.

```bash
BRANCH=$(yakanban branch 42 --claim "$AGENT")
git worktree add ../app-task-42 -b "$BRANCH" origin/main
```

`init` asks which branching model the repository follows — trunk-based, GitHub
flow, git flow, GitLab flow or OneFlow — and every branch name after that comes
from the board, so every agent on the repository uses the same convention. See
[branches and branching models](docs/branching.md).

## Configuration

`.yakanban.yml` is committed and holds no secrets — only the vocabulary and
which backend to talk to: the columns and what they mean, priorities, classes
of service, WIP limits, which columns require a claim, the claim timeout and
the branching model.

Columns belong to the tracker. Add, rename or reorder one in the project's own
UI and yakanban follows on the next command — no re-init, no edit required. See
[configuration](docs/configuration.md) for the full descriptor and
[changing the columns](docs/configuration.md#changing-the-columns).

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

## Other backends

GitHub and GitLab are registered adapters. A backend is one package
implementing `core.Provider` plus one line in the registry — see
[docs/architecture.md](docs/architecture.md#adding-a-provider). Jira, Plane and
Linear are the intended next ones. Nothing provider-shaped leaks into the
model: optional fields stay optional, and anything backend-specific lives in
`Metadata map[string]any`.

## Roadmap

The board runs on yakanban itself:
[github.com/users/aramponi/projects/2](https://github.com/users/aramponi/projects/2).

## The landing page

[aramponi.github.io/yakanban](https://aramponi.github.io/yakanban) is generated
from this repository at deploy time, and nothing generated is committed:

```bash
make site      # ./site/public/{index.html,llms.txt}
```

The install commands, the command table and the exit codes are lifted out of
this README, which carries invisible `<!-- site:... -->` markers naming what
the page takes. Every terminal block on the page is captured by running the
binary against the board. `site/content.md` holds the page's own prose and
nothing that is written here as well, so there is no second copy to keep true.

The extraction is assertive: a marker that moves, or a section asking for a
capture nobody produces, fails generation rather than publishing a page with a
hole in it. `go test ./internal/site` runs that same build against the real
sources, so a pull request that breaks it never reaches Pages.

`llms.txt` is served at the site root for agents that want the contract rather
than the page — the command reference, the exit codes, both skill files and
the backend mapping.

## Development

```bash
make test      # go test ./...
make build     # ./bin/yakanban
make check     # fmt, vet, lint, test
make site      # the landing page and llms.txt
```

`make check` runs golangci-lint, pinned to the same version as CI:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0
```

## License

MIT — see [LICENSE](LICENSE).
