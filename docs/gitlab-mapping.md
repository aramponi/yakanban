# GitLab mapping — design pending live verification

This is the proposed mapping for #7, reviewed against GitLab's official
API documentation on 2026-09-05. **It is not a verified adapter contract.**
No authenticated GitLab test project was available during this work. Every
live check below is pending; documentation examples are not captured fixtures.
Implementation ticket #9 must not be called complete on this evidence.

## Board ownership

Read GitLab's project issue board. Its ordered label lists own the status
vocabulary; `.yakanban.yml` adds local policy, never a competing column list.
Adopt an explicitly selected board when several exist. `init` may provision
labels and lists on a new board, but must not rewrite an adopted board.

Open and Closed are GitLab's implicit endpoints. Closed issues map to Closed;
open issues without a board label map to Open. An open issue with exactly one
board label maps to that label's list. If several board labels apply, refuse
with the issue IID and conflicting labels: guessing a column hides ambiguity.
Reject scoped/filtered boards and non-label lists until their membership
semantics are implemented. Do not silently show the whole project instead.
The [boards API](https://docs.gitlab.com/api/boards/) describes the lists and
board scope; endpoint visibility and filtering still need live verification.

On Free, replace only the current board labels when moving an issue, retain
unrelated labels, and read back the result. This is optimistic coordination,
not atomic exclusivity: a racing writer or web edit can produce conflicting
labels or overwrite a move. Premium/Ultimate scoped labels improve exclusivity
only when all status labels share the same scope. They do not make an arbitrary
adopted board exclusive. These are limitations to expose, not to conceal.
See [scoped labels](https://docs.gitlab.com/user/project/labels/#scoped-labels).

## Field table

All mappings in this table require live validation. “Cannot” is a deliberate
adapter limitation, not a claim that no GitLab API could ever express it.

| Domain field | Free | Premium / Ultimate | Decision and limitation |
|---|---|---|---|
| ID | issue `iid` | same | Project-local IID, never global issue `id` |
| Title, Body | title, description | same | Preserve user content; no hidden metadata block |
| URL | `web_url` | same | Backend URL |
| Created, Updated | `created_at`, `updated_at` | same | Read-only |
| Tags | issue labels | same | Exclude labels used for status, priority and class; preserve unrelated labels on writes |
| Assignees | native assignee | native assignees | Free multi-assignee restrictions must be probed; reject unsupported cardinality before writing |
| Status | board label lists and issue state | same; scoped labels where present | Backend owns vocabulary; conflicting labels are an error |
| Priority | `priority::VALUE` plain labels | same, with native scope exclusivity | Prefix is a convention on Free, not a lock |
| Class | `class::VALUE` plain labels | same, with native scope exclusivity | Vocabulary from project/group labels; same race limit as priority |
| Estimate | native `time_stats` and time-estimate endpoints | same | GitLab duration grammar, not an arbitrary string label |
| Due | native `due_date` | same | Date only; explicit clearing must be verified |
| Started | cannot | cannot in this REST mapping | Do not insert editable timestamps in description or fabricate from `updated_at` |
| Completed | read native `closed_at`; cannot write an arbitrary date | same | Close/reopen controls native timestamp; it is not an independently writable workflow date |
| Parent | cannot in this issue-only REST mapping | cannot in this mapping | Work-item hierarchy requires a separate design; epics are explicitly out of scope |
| DependsOn | cannot preserve direction using `relates_to` | native `is_blocked_by` issue links | Same-project dependencies only; cross-project links remain metadata and must not become local IIDs |
| Blocked reason | cannot | cannot in this mapping | A label has no reason text; do not equate a dependency with a human blocking reason |
| Claim (agent and expiry) | cannot | cannot | No native expiring claim selected; refuse agent workflows explicitly |
| Metadata | issue ID, project ID, state, raw links/time data | same | Extra backend data, not new domain fields |
| Delete/archive | native issue deletion, subject to permission; no issue archive selected | same | Advertise real delete only if validated; do not describe closing as archiving |
| Linked branch | cannot in this mapping | cannot in this mapping | Creating a ref is not the linked-branch contract; merge requests are out of scope |

Native issue content, dates, assignment and time tracking are described by the
[issues API](https://docs.gitlab.com/api/issues/). Dependency operations are
specified by the [issue links API](https://docs.gitlab.com/api/issue_links/);
[linked issue tiers](https://docs.gitlab.com/user/project/issues/related_issues/)
must also be checked when capturing the Free refusal.

## Claims: refuse rather than hide a lock in user content

Claims remain unsupported on every tier in this proposal. Scoped labels cannot
store an expiry. An editable description marker risks removal, malformed data
and lost user edits. Marker comments avoid rewriting the description but still
allow duplicates, deletion and races, and require a new ordering/ownership
protocol. Neither is a backend lock. Choosing a marker now would reverse the
GitHub mapping's decision without solving its failure modes.

`pick`, `--claim`, claim renewal and release must return `ErrUnsupported` with
a reason explaining that GitLab has no selected native agent/expiry storage.
An upgrade must not be suggested for a feature unsupported on every tier.

## Runtime capability discovery

Resolve capabilities and refusal reasons with board metadata, cache them with
that metadata, and invalidate them on refresh. Distinguish Free, paid and
unknown: a denied probe does **not** prove an instance is Free. Permission,
feature flags, deployment version and subscription can all affect an endpoint.

The [namespaces API](https://docs.gitlab.com/api/namespaces/) can expose plan
information in permitted contexts. The self-managed
[license API](https://docs.gitlab.com/api/license/) is administrator-only; it
cannot be a mandatory call for a normal project user. A runtime adapter must
name an unknown entitlement or permission restriction honestly instead of
inventing a Free tier. Do not probe capabilities by mutating a user's issues.

## Authentication and configuration

Use `GITLAB_TOKEN` first, otherwise the existing `glab` login for the configured
host. Do not persist or print credentials. Use a provider `host` setting for
self-managed GitLab, defaulting to `gitlab.com`, plus project namespace/path
and board ID. Never send a host's credential to a different host on redirect.
The exact token retrieval command and self-managed URL handling must be tested
against the installed [GitLab CLI](https://docs.gitlab.com/cli/).

## Planned API traffic (not measured)

| Operation | Expected uncached calls |
|---|---|
| Board | Project + board/lists + paginated labels + permitted entitlement lookup; exact count depends on response shape |
| List | Board metadata + one issue-list GET per page; paid dependency mapping adds one links GET per issue/page |
| Get | Board metadata + issue GET; links GET when supported |
| Create | Issue POST, optional estimate POST, optional dependency POSTs, final readback |
| Update/move | Read current issue, optional issue PUT, estimate/reset call, dependency link mutations, readback |
| Delete | Issue DELETE if permission and semantics are verified |
| Init | Lookup/adopt board, or create board and missing labels/lists; never rewrite adopted columns |

Board and list reads use the existing decorator. Get and writes stay live.
Multi-call operations are not transactions: identify an already-created issue
in a partial-failure error, and never blindly retry its POST.

## Findings that block #9's unchanged-port acceptance criterion

The current `init` hardcodes GitHub owner/repository/project-number settings,
and has no generic provider option input despite `BootstrapOptions.Options`.
Self-managed GitLab provisioning cannot be registered as just an adapter today.
A generic provider settings mechanism should be agreed before implementing it.

The service stamps Started/Completed as writable fields on transitions, but
GitLab's native closed timestamp is read-only and no Started mapping is chosen.
The current capability vocabulary has no way to express this distinction.
The port needs a provider-agnostic policy for automatic timestamps versus
explicit date edits; silently dropping them in an adapter would lose data.

## Required live evidence before settling the mapping

Capture complete request/response bodies (without tokens), status codes and
pagination headers from a disposable, explicitly authorized project. Record
host/version, project tier and caller role alongside fixtures in `testdata/`.

- [ ] Adopt a board; add/reorder a column in the web UI and refresh it here.
- [ ] Capture Open/Closed, hidden endpoints, label lists and scoped boards.
- [ ] Create/get/list/update/delete issues; verify null fields and empty lists.
- [ ] Preserve unrelated labels; reproduce conflicting Free status labels.
- [ ] Set/reset native estimate and due date; exercise invalid input.
- [ ] Set/unset assignees and verify Free cardinality refusal.
- [ ] Capture Free directional-link refusal; do not use `relates_to` as fallback.
- [ ] Capture paid directional links and scope exclusivity before advertising them.
- [ ] Verify plan discovery with a normal project user's permissions.
- [ ] Close/reopen and observe native timestamps; resolve the port finding above.
- [ ] Verify environment-token and glab authentication, including self-managed host.
- [ ] Replay captured responses offline, including partial success, denied writes,
      null payloads and pagination, through the real mapping code.
