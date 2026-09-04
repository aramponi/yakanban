---
name: yakanban
description: >
  Manage tickets with yakanban, a CLI Kanban board backed by a real tracker
  (GitHub Issues + Projects v2). Use when the user mentions tasks, kanban,
  board, backlog, sprint, tickets, issues, priorities, blockers, or wants to
  track, create, list, move, edit or close work items. Also use for standup,
  status update, sprint planning, triage or board metrics.
allowed-tools:
  - Bash(yakanban *)
  - Bash(gh yakanban *)
---

# yakanban

Drive a Kanban board hosted in GitHub Issues + Projects v2. The backend is the
source of truth; humans work in the GitHub web UI, you work here. Reads are
cached locally for a minute, writes always go straight through.

## Current board state

!`yakanban board --compact 2>/dev/null || echo 'No board configured — run: yakanban init'`

## Rules

- Use `--compact` for `list` and `board`: one tab-separated line per record.
- Use `yakanban show ID` (default format) to read a task; add `--json` only
  when piping into another tool.
- **Never hardcode statuses, priorities or classes.** They come from the
  project itself — read them from the board state above or `yakanban config`.
- Task IDs are GitHub issue numbers.
- Dates are `YYYY-MM-DD`.
- Always pass `--yes` to `delete` (it closes and archives, it does not erase).
- Get one agent name per session with `yakanban agent-name` and reuse it.

## Decision tree

| User wants to…                      | Command |
|---|---|
| Board overview / standup            | `yakanban board --compact` |
| Get an agent name for claims        | `yakanban agent-name` |
| List everything                     | `yakanban list --compact` |
| List by status                      | `yakanban list --compact --status todo,in-progress` |
| List by priority                    | `yakanban list --compact --priority high,critical` |
| List by tag / assignee              | `yakanban list --compact --tag bug --assignee alice` |
| List blocked work                   | `yakanban list --compact --blocked` |
| List what is ready to start         | `yakanban list --compact --status todo --unclaimed --not-blocked` |
| List tasks whose deps are done      | `yakanban list --compact --unblocked` |
| Search                              | `yakanban list --compact --search "login"` |
| Read one task                       | `yakanban show ID` |
| Create a task                       | `yakanban create "TITLE" --priority high --tags bug` |
| Create and claim at once            | `yakanban create "TITLE" --claim $AGENT` |
| Claim the next available task       | `yakanban pick --claim $AGENT --status todo --move in-progress` |
| Start working on a known task       | `yakanban move ID in-progress --claim $AGENT` |
| Advance / step back a column        | `yakanban move ID --next` / `--prev` |
| Complete a task                     | `yakanban move ID done` |
| Edit fields                         | `yakanban edit ID --title "NEW" --priority high` |
| Add / remove tags                   | `yakanban edit ID --add-tag bug --remove-tag ui` |
| Set a due date                      | `yakanban edit ID --due 2026-03-01` |
| Block / unblock                     | `yakanban edit ID --block "REASON"` / `--unblock` |
| Add a dependency / a parent         | `yakanban edit ID --add-dep 12 --parent 7` |
| Append a progress note              | `yakanban edit ID -a "note" -t --claim $AGENT` |
| Create a branch for a task          | `yakanban branch ID --claim $AGENT` |
| Park a task for someone else        | `yakanban handoff ID --claim $AGENT --note "…" -t --release` |
| Release a claim                     | `yakanban edit ID --release` |
| Close a task                        | `yakanban delete ID --yes` |
| See changes made in the web UI      | `yakanban sync` |
| Inspect the configuration           | `yakanban config` |
| See a column somebody just added    | `yakanban board --refresh` |
| Set up a new board                  | `yakanban init` |

## Agent session

```bash
AGENT=$(yakanban agent-name)                 # once per session, remember it

yakanban board --compact                     # orient
yakanban pick --claim "$AGENT" --status todo --move in-progress   # atomic

yakanban edit 42 -a "Reproduced; stale session cookie." -t --claim "$AGENT"

# Park for a human decision
yakanban handoff 42 --claim "$AGENT" --block "Needs a product call" \
  --note "Waiting on: …" -t --release

# Finish
yakanban edit 42 --release && yakanban move 42 done
```

## Exit codes

`0` ok · `2` bad input or no board · `3` not found · `4` auth or missing
`project` scope · `5` claimed by another agent · `6` unsupported by the backend.

On `4`, tell the user to run `gh auth refresh -s project`. On `5`, pick a
different task — `--force` is for a human unsticking the board, not for you.

## Pitfalls

- **DO** use `--compact` for listings; it is the cheapest readable output.
- **DO** pass `--claim $AGENT` on every write you make to a task you are
  working on: it renews the claim so it does not expire mid-task.
- **DO** use `-a` / `--append-body` for notes — `--body` replaces the whole body.
- **DO** take work with `pick` (next available) or `move ID <column> --claim`
  (a named ticket). Both verify the claim survived the write, so both are safe
  when other agents share the board. `pick` cannot target an ID — that is what
  `move` is for.
- **DO NOT** take work with list → edit → move: nothing there checks that the
  task is still yours by the time you write.
- **DO NOT** use `--json` unless you are parsing the output.
- **DO NOT** assume a column is called `todo` or `in-progress`; read the board.
- **DO NOT** use `--next` / `--prev` without knowing the current column: they
  fail at the first and last column.
- **DO** quote titles containing special characters.
