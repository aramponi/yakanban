# GitLab mapping

This mapping was exercised on 2026-09-05 (2026-09-04 UTC) against the authorized
`aramponi/mysandbox` project on gitlab.com, running GitLab 19.4.0-pre. The
namespace API reports `plan: free`; the caller has Owner access. Complete
responses were captured for offline replay, with token fields redacted.
Premium/Ultimate and self-managed installations were not available for live
testing; their documented behavior is identified separately below.

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

Free issue/board/label/date/estimate/delete mappings below were exercised live.
Paid-tier mappings are documentation-derived. “Cannot” is a deliberate
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

Claims remain unsupported on every tier in this mapping. Scoped labels cannot
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

## Port findings and generic changes required by #9

The current `init` hardcodes GitHub owner/repository/project-number settings,
and has no generic provider option input despite `BootstrapOptions.Options`.
Self-managed GitLab provisioning cannot be registered as just an adapter today.
The implementation will add generic `init --set key=value` provider settings.
This is an explicit CLI port finding, not a GitLab-only switch.

The service stamps Started/Completed as writable fields on transitions, but
GitLab's native closed timestamp is read-only and no Started mapping is chosen.
The current capability vocabulary has no way to express this distinction.
Ticket #8 adds a generic workflow-date capability: automatic stamps are only
written when supported, and explicit date edits otherwise fail with a reason.
GitLab reads `closed_at` after close/reopen and never silently drops explicit
date edits.

## Live evidence and remaining coverage

Captured full responses and pagination headers are replayed by the adapter tests.
The sandbox remains available for repeatable manual validation; tests never use
its credentials or contact it. Evidence captured so far:

- Board creation, label-list ordering, implicit Open/Closed endpoints.
- Create/get/list/update/delete, including 204 deletion and subsequent 404.
- Free accepted both Doing/Review and priority::medium/priority::high labels
  simultaneously: neither field is exclusive on this plan.
- A directional issue-link POST returned **403**, with the message
  `Blocked issues not available for current license`; relates_to returned 201.
- Native 4h estimate, reset, and invalid estimate (400).
- Due-date clearing with an empty string, assignee clearing with an empty array.
- Close/reopen returned and then cleared native closed_at.
- Issue-list pagination returned X-Next-Page; page two contained the other issue.
- Namespace plan discovery succeeded for the authorized Owner account.
- glab config get token --host gitlab.com returned the existing login token
  without persisting a yakanban credential.

Remaining live coverage: paid dependency writes, paid scope exclusivity,
self-managed authentication, non-Owner plan visibility, and multi-assignee
license behavior. These must not be described as live-verified. The adapter
will refuse unavailable or unknown capabilities with the precise known cause.
