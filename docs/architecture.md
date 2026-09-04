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
    Capabilities() Capability
    Board(ctx context.Context) (*BoardInfo, error)
    List(ctx context.Context, filter Filter) ([]Task, error)
    Get(ctx context.Context, id string) (*Task, error)
    Create(ctx context.Context, draft Draft) (*Task, error)
    Update(ctx context.Context, id string, patch Patch) (*Task, error)
    Delete(ctx context.Context, id string) error
}
```

Three optional interfaces sit next to it: `Bootstrapper` (a backend that can
provision its own board, used by `yakanban init`), `SettingsWriter` (hand back
the config block to persist afterwards) and `Invalidator` (drop a cache).

### Capabilities rather than a lowest common denominator

Not every tracker can express every field. Instead of shrinking the model to
what all of them share, a provider advertises what it supports:

```go
func (p *Provider) Capabilities() core.Capability {
    return core.CapClaims | core.CapDependencies | core.CapParent |
        core.CapBlocked | core.CapEstimate | core.CapClass | core.CapDueDate
}
```

The service checks the patch against them and fails with a clear message —
`provider X cannot store dependencies` — instead of silently dropping data.

Anything a backend knows and the domain does not goes into
`Task.Metadata map[string]any` (the GitHub adapter puts the project item ID and
the issue open/closed state there).

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
3. Report honest `Capabilities()`.
4. Wrap backend errors around the sentinels in `core/errors.go`
   (`ErrNotFound`, `ErrAuth`, `ErrClaimed`, …) so exit codes stay meaningful.
5. Implement `Bootstrapper` if `yakanban init` should be able to create the
   board.
6. Add one entry to `internal/registry/registry.go`.

Nothing in `internal/core`, `internal/app` or `internal/cli` changes.

## Testing

- `internal/core`, `internal/app`: pure unit tests, with an in-memory provider
  that records the patches it receives.
- `internal/provider/github`: an `httptest` server replaying real GitHub
  payloads, asserting which call goes to REST and which to GraphQL.
- `internal/provider/cached`: a counting provider proving what is cached and
  what is not.

No test touches the network.
