# Working with AI agents

yakanban is designed so that several agents — and the humans around them — can
share one board without stepping on each other.

## The contract

1. **Get a name once per session.** `yakanban agent-name` prints something like
   `frost-maple-07`. Reuse it for the whole session.
2. **Claim before working.** A claim expires (default one hour), so a crashed
   agent releases its work automatically.
3. **Leave a trail.** `yakanban edit ID -a "note" -t` appends a timestamped
   note to the issue body — visible to everyone in the GitHub UI.
4. **Release when parking.** `--release` hands the task back.

## Session shape

```bash
AGENT=$(yakanban agent-name)

# Orient
yakanban board --compact

# Take the top of the list — atomic, race-safe
yakanban pick --claim "$AGENT" --status todo --move in-progress

# Report progress without losing the body
yakanban edit 42 -a "Reproduced; the session cookie is dropped on redirect." -t --claim "$AGENT"

# Park for a human decision
yakanban handoff 42 --claim "$AGENT" \
  --block "Needs a product call on the redirect target" \
  --note "Waiting on: which URL to land on after login." -t --release

# Or finish
yakanban edit 42 --release
yakanban move 42 done
```

## Token-efficient output

Use `--compact`: one tab-separated line per task, no colour, no wrapping.

```console
$ yakanban list --compact --status in-progress
42  In Progress  high  Fix the login redirect  #bug,#auth  @alice  claim:frost-maple-07
57  In Progress  low   Update the CI matrix                        blocked:waiting on infra
```

Use `--json` only when piping into another tool.

## Why `pick` rather than list-then-claim

GitHub has no compare-and-swap, so `pick` claims optimistically and reads the
task back to confirm the claim is ours. A candidate another agent took in the
meantime comes back as a conflict and the next candidate is tried. Listing
first and claiming afterwards has no such guard: two agents that list at the
same moment both believe they own the task.

## Handling conflicts

A write with `--claim` on a task another agent holds exits with code 5 and a
message naming the holder and the expiry. The right reaction is to pick another
task, not to `--force`: `--force` exists for a human unsticking a stuck board.

## Claude Code / skills

A minimal skill body is enough, since the CLI is self-describing:

```markdown
Run `yakanban board --compact` first to learn the board's columns and
priorities — never hardcode them. Use `--compact` for every listing. Take work
with `yakanban move ID <column> --claim $AGENT`, where `$AGENT` comes from a
single `yakanban agent-name` call at the start of the session.
```
