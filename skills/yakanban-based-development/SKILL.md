---
name: yakanban-based-development
description: >
  Autonomous, parallel-safe development workflow using yakanban.
  Use when the user asks to work through tasks, do kanban-based development,
  or when multiple agents need to coordinate work on the same codebase.
  Optimized for explicit handoffs and a "defer to user" protocol when
  human intervention is required.
allowed-tools:
  - Bash(yakanban *)
  - Bash(gh yakanban *)
  - Bash(git *)
  - Bash(go *)
  - Bash(golangci-lint *)
  - Bash(awk *)
---

# yakanban-Based Development

Autonomous, parallel-safe development using `yakanban` to coordinate work on a
shared board. Claims prevent duplicate work; the review column is the waiting
room (handoff, user action, merge, decisions).

## Multi-Agent Environment

**This board is shared, and it lives in the tracker — not in this checkout.**
Other agents, and humans working in the GitHub web UI, may be changing it while
you work. This means:

- Another agent may claim a task between the time you list it and try to take it.
- A human may reorder columns, add tasks or close them from the browser.
- What you saw a moment ago may already be stale.

The **claim** is the coordination primitive: a soft lock, held by an agent
name, that expires on its own (`claim_timeout`, one hour by default). **You
MUST claim a task before starting any work on it, and you MUST only take
unclaimed tasks.** Violating this causes duplicate work and wasted effort.

Reads are cached locally for a minute. If you need to see someone else's change
right now, run `yakanban sync` or add `--refresh` to any command. Writes are
never cached — they go straight to the tracker.

## Non-Negotiables

- **Claim before you change anything.** No task edits, no code changes.
- **One active task per agent.** Keep at most one task in progress per session.
- **Use `pick`, never list-then-claim.** `pick` detects a task another agent
  took in between; listing first and claiming afterwards does not.
- **Never steal a live claim.** If it is claimed, take something else.
  `--force` exists for a human unsticking the board, not for you.
- **Never release someone else's claim.** Only `--release` your own work, or
  when the user explicitly asks.
- **Always leave a handoff.** Before parking a task, say where the work stands.
- **Refresh long claims.** Passing `--claim <agent>` on any write renews it;
  do that on your progress notes so a long task does not expire mid-flight.

## Where you run yakanban

Anywhere in the repository, including inside a task worktree: `.yakanban.yml`
is tracked, and the board itself lives in the tracker, so every checkout talks
to the same board. There is no "board home" to return to, and **no board files
to commit** — that whole step disappears compared with a file-based board.

Still do code changes in a task worktree, not in the main checkout.

Sanity check at the start of a session:

```bash
yakanban board --compact    # confirm the board name and its columns
```

**Never hardcode column names.** They come from the tracker and a human may
rename them. Read them from `yakanban board` or `yakanban config`. Matching is
forgiving about case and separators, so `in-progress` finds `In Progress`.

## Defer-to-User Boundary (exceptions)

By default, take tasks all the way to done (worktree → commit → merge → done).

Hand off to the user (park the task in the review column) only when you need:

- an important product/spec decision with several valid options and no clear winner
- credentials, access or external actions (push, releases, deployments, env vars)
- a merge conflict that needs judgement, not mechanical resolution
- repeated test/lint failures you cannot resolve

## Agent Identity (for claims)

Generate one name per session, at the very start:

```bash
yakanban agent-name       # e.g. frost-maple-07
```

**Remember it in your context** and use it as a literal string in every claim
command for the rest of the session. Do not put it in a file or an environment
variable — neither is isolated between agents.

## Default Loop (worktree → merge → done)

Use `--compact` for board and list output to keep things short.

Start from a clean main:

```bash
git switch main
git status
```

### 1) Pick and claim (atomically)

```bash
yakanban pick --claim <agent> --status todo --move in-progress
```

If the todo column is empty:

```bash
yakanban pick --claim <agent> --status backlog --move in-progress
```

`pick` takes the highest-priority task that is unclaimed, unblocked and whose
dependencies are all done, claims it, and reads it back to confirm the claim is
yours. A task somebody took in the meantime is skipped automatically. It prints
the full task, so there is usually no need to `show` it again.

### 2) Create a worktree

```bash
git worktree add ../task-<ID> -b task/<ID>-<kebab-description>
cd ../task-<ID>
```

Skip the worktree only for work that cannot conflict (a research note, no
tracked files). If you touch tracked code or config, use one.

### 3) Implement, test, commit

Implement the smallest change that satisfies the task.

- Bugs: write a failing test first, then fix.
- Run the project's checks, commonly:
  - `go test ./...`
  - `golangci-lint run ./...`

```bash
git add <files>
git commit -m "feat: <description>"
```

### Progress notes (recommended)

Leave short timestamped notes as you go — they become issue comments the whole
team can read, and they renew your claim:

```bash
yakanban edit <ID> -a "Implemented X/Y/Z, running tests." -t --claim <agent>
```

`-a` / `--append-body` appends without replacing the body; `-t` stamps the
note. **Never use `--body` for a note** — it replaces everything.

### 4) Merge to main

```bash
cd <main checkout>
git switch main
git status
git merge task/<ID>-<kebab-description>
go test ./...
golangci-lint run ./...
```

If `git status` shows unexpected changes or an operation in progress, do not
force it. Park the task and move on:

```bash
yakanban handoff <ID> --claim <agent> \
  --note "Ready to merge: task/<ID>-…; remaining: …" -t --release
```

### 5) Mark done (only after the merge is on main and checks pass)

```bash
yakanban edit <ID> --release
yakanban move <ID> done
```

Moving into the terminal column stamps `Completed` for you; moving out of the
intake column stamped `Started` when you picked it up.

### 6) Optional cleanup

```bash
git worktree remove --force ../task-<ID>
git branch -d task/<ID>-<kebab-description>
```

There is no board commit to make: the board is in the tracker.

## Blocked / Needs User Input (the "park and move on" rule)

When you cannot continue without the user — a decision, access, environment,
anything outside your control — one command parks the task, records why, and
lets go of the claim:

```bash
yakanban handoff <ID> --claim <agent> \
  --block "Waiting on user: <what you need>" \
  --note "## Handoff
- Current state:
- Branch (if any):
- Open questions (A/B):
- Next step:" \
  -t --release
```

In the note, include the exact question (prefer A/B options), what you already
tried and what happened, and the minimal next step once the user answers.

Then pick the next task. Do not idle.

## Resuming a parked task

```bash
yakanban edit <ID> --unblock --claim <agent>
yakanban move <ID> in-progress --claim <agent>
```

## Column meanings (keep the board honest)

| Column | Meaning |
|---|---|
| in progress | Actively being worked by an agent right now |
| review | Waiting: ready to merge, or blocked on a user, decision or unblock |
| done | Merged to main, checks passing |

The exact names come from the board; these are the roles they play.

## When there is nothing to pick

If `pick` exits 3 (`no unblocked, unclaimed task to pick`):

- Check blocked work: `yakanban list --compact --blocked`
- Check the waiting room: `yakanban list --compact --status review`
- Someone may have added work in the browser: `yakanban sync` and look again
- If everything is waiting on the user, ask targeted questions and stop —
  do not thrash the board.

## Exit codes worth branching on

`0` ok · `2` bad input or no board · `3` nothing found · `4` auth or a missing
`project` scope (tell the user: `gh auth refresh -s project`) · `5` claimed by
another agent (take something else) · `6` the backend cannot store that field.
