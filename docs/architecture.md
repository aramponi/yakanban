# Architecture

yakanban is a hexagonal (ports-and-adapters) application. The rule is one way:
**the domain imports nothing**.

```
cmd/yakanban ──▶ internal/cli ──▶ internal/app ──▶ internal/core
                      │                                 ▲
                      │                                 │ implements
                      └──▶ internal/registry ──▶ internal/provider/github
                                                internal/provider/cached
```

| Package | Role |
|---|---|
| `internal/core` | Domain: `Task`, `Draft`, `Patch`, `Filter`, `BoardInfo`, and the `Provider` port. No I/O, no dependencies. |
| `internal/app` | Use cases: defaults, vocabulary validation, claim ownership, `Started`/`Completed` stamping, `--next`/`--prev` navigation. |
| `internal/cli` | Cobra commands. Parse flags, call the service, print. No business rules. |
| `internal/provider/github` | Driven adapter: GitHub Issues (REST) + Projects v2 (GraphQL). |
| `internal/provider/cached` | Decorator adding the local read cache around any provider. |
| `internal/registry` | The only package that knows every adapter. |
| `internal/config` | The committed `.yakanban.yml` descriptor. |
| `internal/output` | Human, `--compact` and `--json` renderers. |

## The provider port

```go
type Provider interface {
    Name() string
    Board(ctx context.Context) (*BoardInfo, error)
    List(ctx context.Context, filter Filter) ([]Task, error)
    Get(ctx context.Context, id string) (*Task, error)
    Create(ctx context.Context, draft Draft) (*Task, error)
    Update(ctx context.Context, id string, patch Patch) (*Task, error)
    Delete(ctx context.Context, id string) error
}
```

Optional interfaces sit next to it: `Bootstrapper` (a backend that can
provision its own board, used by `yakanban init`), `SettingsWriter` (hand back
the config block to persist afterwards), `Invalidator` (drop a cache) and
`Brancher` (attach a source branch to a task).

`Brancher` is deliberately not part of `Provider`. Linked branches are a git
forge feature; Jira and Linear have branch integrations, but not this model,
and the domain must not assume every tracker sits on a git host.

Because the cache decorates the provider, an optional interface has to be
looked for *underneath* the decorator — a decorator must never answer for a
capability its inner provider lacks. `core.AsBrancher` walks the `Unwrap()`
chain to do that; new optional ports should follow the same shape.

### Capabilities rather than a lowest common denominator

Not every tracker can express every field. A provider resolves a
`BoardInfo.Capabilities` set alongside the live schema. It contains supported
bits and a backend explanation for each unavailable feature:

```go
board.Capabilities = &core.CapabilitySet{
    Supported: core.CapEstimate | core.CapClass | core.CapDueDate,
    Reasons: map[core.Capability]string{
        core.CapDependencies: "dependencies need Premium; this namespace is Free",
    },
}
```

The board cache persists the set and its reasons together; refresh invalidates
both. `yakanban config` displays named supported/unsupported features in human
and JSON output. An unavailable schema is an error, never a guessed licence.

Create and edit validate requested fields before writing. Branch/delete/pick
operations also check their capabilities. Automatic workflow timestamps are
only written when `CapWorkflowDates` is present; explicit date edits otherwise
fail. GitLab's native `closed_at` can still be read without claiming it is
writable. GitHub retains all of its previous capabilities and timestamp behavior.

The old `Capabilities() Capability` method remains an optional compatibility
bridge for existing adapters. New adapters implement the board metadata contract;
the service no longer requires a compile-time capability constant.

Anything a backend knows and the domain does not goes into
`Task.Metadata map[string]any` (the GitHub adapter puts the project item ID and
the issue open/closed state there).

## Branching models

`core.BranchPolicy` is board policy, like the column list: which branch work
starts from, which it merges back into, and how a task becomes a branch type.
`internal/config` resolves it from a preset name plus overrides; the domain
only ever sees the resolved policy, so adding a model is a table entry rather
than a code path.

The models stop at naming and routing. Release branches, and the second merge
a git flow hotfix needs, are release engineering: half-automating them would
silently lose a fix, so the policy carries a warning to print instead.

## Caching

The choice is deliberate and narrow:

- **Writes always go straight through.** There is no write queue, no local
  mutation log, nothing to reconcile.
- **`List` and `Board` are cached** on disk under `.yakanban/cache/`, keyed by
  the filter, with a TTL (default 1 minute).
- **`Get` is never cached.** The service reads a task immediately before
  patching it; a stale read there would clobber someone else's change.
- Every write invalidates the whole cache.

The result: an agent can call `yakanban list` in a loop without spending API
quota, and two agents can work the same board without a merge algorithm.

## Claims

GitHub has no notion of an expiring lock, so claims are two ordinary project
fields, `Claim` (agent name) and `Claim Expires` (RFC 3339). They are
advisory: a write carrying `--claim` fails when another agent holds an
unexpired claim, while a human edit without `--claim` is never blocked. This is
coordination between agents, not access control.

It is not a distributed lock. Two agents claiming the same task within the same
round trip can both win; the loser notices at the next read. For a handful of
agents on one board, that is the right trade for staying inside GitHub's data
model.

## Adding a provider

1. Create `internal/provider/<name>/` with a type implementing `core.Provider`.
2. Map the domain onto the backend, and put whatever is left in `Metadata`.
3. Resolve honest `BoardInfo.Capabilities` and backend refusal reasons.
4. Wrap backend errors around the sentinels in `core/errors.go`
   (`ErrNotFound`, `ErrAuth`, `ErrClaimed`, …) so exit codes stay meaningful.
5. Implement `Bootstrapper` if `yakanban init` should be able to create the
   board.
6. Add one entry to `internal/registry/registry.go`.

Adapters should not require provider-specific domain or service changes. The
GitLab implementation exposed two existing port gaps: automatic workflow dates
are now optional, and `init --set key=value` passes generic provider settings.
Remote detection accepts nested namespaces on any forge; none of these paths
branches on a GitLab provider name. These findings are recorded in
[gitlab-mapping.md](gitlab-mapping.md).

## Testing

- `internal/core`, `internal/app`: pure unit tests, with an in-memory provider
  that records the patches it receives.
- `internal/provider/gitlab`: captured GitLab Free response envelopes replayed
  through an `httptest` server, with separately identified paid/fault injections.
- `internal/provider/github`: an `httptest` server replaying real GitHub
  payloads, asserting which call goes to REST and which to GraphQL.
- `internal/provider/cached`: a counting provider proving what is cached and
  what is not.

No test touches the network.
