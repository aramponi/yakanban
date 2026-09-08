# Configuration

`.yakanban.yml` is committed next to your code. It holds no secrets — only the
vocabulary and which backend to talk to:

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

`init` writes this file and adds `.yakanban/` (the read cache) to `.gitignore`.

## Changing the columns

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

## Branching

The branching model lives in the same file, under `branching:`. See
[branching](branching.md) for the presets, the templates and what is
deliberately left to you.
