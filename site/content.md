<!--
The landing page's own prose, and nothing else.

Facts belong in README.md: install commands, the command list and the exit
codes are lifted out of it at build time, and every terminal block on the page
is captured by running the binary. Nothing written here is also written there,
so there is no second copy of anything that can quietly stop being true.

`sample: NAME` renders the captured output of that command in the section.
`table: ID`   renders the README table marked with that id.
-->

# yakanban

## Hero

sample: board

Yet another Kanban. The name is accurate and the joke is over; what follows is
the part that is not funny.

## It borrows the login you already have

sample: auth

yakanban stores no credentials. It reads `$YAKANBAN_GITHUB_TOKEN`, `$GH_TOKEN`
or `$GITHUB_TOKEN`, and when none of them is set it asks the
[GitHub CLI](https://cli.github.com) for the token you are already using:
`gh auth token`. So `gh` is a prerequisite for the ordinary path, and the
error says so rather than failing somewhere deeper.

That is the whole authentication story, and it is deliberately not ours. There
is no login command, no keychain entry and no token file to leak, because the
credential belongs to a tool that already manages it — and in CI, where `gh`
is not the natural answer, one environment variable takes over.

One thing will catch you once. Projects v2 sits behind its own OAuth scope, so
an account that authenticated with `gh` before ever touching projects needs it
added:

```bash
gh auth refresh -s project
```

Both cases — no credential at all, or a credential without the scope — exit 4,
and in both the message is the instruction:

## Two kinds of user, one board

A developer and an agent work in a terminal. A product owner works in a
browser, on the GitHub project board they were already using. Neither of them
is looking at a copy.

yakanban is a client, not a store. Every write goes straight to the tracker;
reads pass through a one-minute cache so an agent can poll a board without
spending someone's API quota. There is no state on disk to reconcile, and
nothing to lose when a laptop dies.

That has a price, and it is worth saying out loud: without a network you have
no board. Offline was traded for never being wrong about who owns what.

## The claim is the whole trick

sample: ready

Two agents pointed at the same backlog will pick the same ticket. A claim is a
soft lock — an agent name and an expiry, written into two ordinary project
fields — so the second agent is refused and goes to find other work.

Ordinary fields, because a human has to be able to see them. Open the project
in the browser and the claim is a column like any other: who holds what, and
until when. Nothing about the coordination is invisible to the person who did
not install anything.

Expiring, because an agent that dies mid-task must not take a ticket with it.
The lock lets go by itself after an hour, and a long task renews it by writing
a progress note.

`pick` claims the next available ticket and reads the claim back before
returning it. That read is the reason several agents on one board is safe:
list-then-claim checks nothing, because the answer is stale by the time it
arrives.

## One shape for every command

table: commands

Fifteen commands, and every one of them takes the same global flags. There is
no command that prints JSON and another that does not.

## Output you can pipe, exit codes you can branch on

sample: compact
table: exit-codes

`--compact` prints one tab-separated line per record, which is what you want
when the reader pays by the token. `--json` prints the whole record when it
does not. Both are on every command, alongside `--table`, `--no-color`,
`--refresh` and `--no-cache`.

The exit codes are the part a script actually reads. Code 5 in particular is
not an error: it means another agent got there first, and the correct response
is to pick something else.

## The instructions ship with the binary

The hard part of handing a board to an agent is not the API, it is telling it
when to claim, when to park a task and when to stop and ask. Two skill files
carry that, and they are compiled into the binary, so a downloaded release
installs them with no checkout.

`skill install` detects Claude Code, Codex, Cursor, Gemini CLI, Antigravity,
Hermes, Pi and OpenClaw, shows you what it found and lets you change its mind
before it writes anything. A file you have edited since is never overwritten
without `--force`: each one carries a hash of the text yakanban wrote, so
"stale" and "you changed this" are different answers.

## What we didn't build

Every one of these was a decision, and each cost something. The reasons are
here because a list of omissions without them is just a list of missing
features.

### No second web UI

The tracker has one, and the people who do not use a terminal are already
logged into it. A web UI of our own would be a second place for the board to
be wrong.

### No local mirror of the board

Tempting, and it would have bought offline reads. It would also have bought a
sync direction, a conflict policy and a class of bug where the agent and the
human disagree about reality. The cache holds reads for a minute and forgets
them; that is the whole of it.

### No release automation, and no git flow back-merge

`branch` creates branches and links them to tickets. It stops there. Cutting
releases is release engineering, and a back-merge that a tool silently skips
is how a hotfix gets lost. yakanban tells you the back-merge is yours and
leaves it undone rather than half done.

### No merge request management

Pull requests belong to the forge, which already reviews them, checks them and
merges them. Wrapping that in another CLI would add a layer whose only job is
to be out of date.

### `delete` closes; it does not destroy

`yakanban delete` closes the issue and archives the board item. The history
stays. An agent with a shell and a bad afternoon should not be able to
permanently remove anybody's work.

### No published `gh` extension

The same binary answers to `gh yakanban ...` when it is installed under the
name `gh-yakanban`, and `make gh-extension` builds it. It is not shipped in
releases: `gh extension install` resolves a *repository* whose name begins
with `gh-`, and this one does not, so a published binary could never be
installed the documented way. It would have been dead weight in every release.

### No provider abstraction written before the second provider

The domain knows nothing about GitHub, and a backend is one package plus one
line in a registry. That is as far as the abstraction goes today, because the
shape of the second one is not knowable from the first.

## For agents

The site serves an [llms.txt](/llms.txt) at its root: the command reference,
the exit codes, the two skill files and the backend mapping, as links an agent
can follow instead of a page it has to scrape.

It is generated from the same sources as this page. A hand-written contract is
the first file to go stale, and a stale contract is worse than none.
