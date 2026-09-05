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
board scope. The observed board has both endpoints visible and no filters;
filtered/hidden boards are refused and covered by synthetic refusal tests.

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
| Assignees | native assignee | native assignees | One assignee on Free/unknown; multiple on verified paid plans; cardinality refusal is documentation-derived |
| Status | board label lists and issue state | same; scoped labels where present | Backend owns vocabulary; conflicting labels are an error |
| Priority | `priority::VALUE` plain labels | same, with native scope exclusivity | GitLab label priority orders importance (0 is highest); unranked ties use label name; no Free lock |
| Class | `class::VALUE` plain labels | same, with native scope exclusivity | Vocabulary from project/group labels; same race limit as priority |
| Estimate | native `time_stats` and time-estimate endpoints | same | GitLab duration grammar, not an arbitrary string label |
| Due | native `due_date` | same | Date only; empty-string clearing verified live |
| Started | cannot | cannot in this REST mapping | Do not insert editable timestamps in description or fabricate from `updated_at` |
| Completed | read native `closed_at`; cannot write an arbitrary date | same | Close/reopen controls native timestamp; it is not an independently writable workflow date |
| Parent | cannot in this issue-only REST mapping | cannot in this mapping | Work-item hierarchy requires a separate design; epics are explicitly out of scope |
| DependsOn | cannot preserve direction using `relates_to` | native `is_blocked_by` issue links | Same-project dependencies only; cross-project blockers are refused rather than misread as local IIDs |
| Blocked reason | cannot | cannot in this mapping | A label has no reason text; do not equate a dependency with a human blocking reason |
| Claim (agent and expiry) | cannot | cannot | No native expiring claim selected; refuse agent workflows explicitly |
| Metadata | issue ID, project ID, state, raw links/time data | same | Extra backend data, not new domain fields |
| Delete/archive | native issue deletion, subject to permission; no issue archive selected | same | Permanent deletion requires active project and Maintainer/Owner permission; 204 then 404 verified |
| Linked branch | cannot in this mapping | cannot in this mapping | Creating a ref is not the linked-branch contract; merge requests are out of scope |

Native issue content, dates, assignment and time tracking are described by the
[issues API](https://docs.gitlab.com/api/issues/). Dependency operations are
specified by the [issue links API](https://docs.gitlab.com/api/issue_links/);
[linked issue tiers](https://docs.gitlab.com/user/project/issues/related_issues/)
explain the Free directional-link refusal captured during validation.

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
The implementation reads `glab auth git-credential get` with the configured
HTTPS host on stdin. It respects glab's keyring and credential expiry; an expired
OAuth credential is refreshed by `glab auth status --hostname HOST` and read
again. An actual expired token was observed during validation and glab renewed
it successfully. Only glab stores its own refreshed credential. This was tested
with glab 1.116.0. **Use Authorization: Bearer** for both
OAuth and personal access tokens: PRIVATE-TOKEN with the actual OAuth token
returned 401, while Bearer succeeded. Both glab lookup and `GITLAB_TOKEN` were
verified with the real binary; credentials never enter the descriptor.
See [REST authentication](https://docs.gitlab.com/api/rest/authentication/).
Self-managed settings accept an HTTPS hostname with optional port; subpath
installations are not supported. Host parsing/redirect refusal are tested
locally, but no self-managed instance was available for live validation.

## API traffic

The schema costs three requests (project, board including lists, namespace)
plus one per label page (100/page). It is cached on disk with the entitlement
result. A captured-response test checks the normal cold Board + Get total of
five calls. The following adapter costs exclude that schema overhead:

| Operation | API calls with schema available |
|---|---|
| Board | Zero until cache expiry/refresh; otherwise three + label pages |
| List | One issue GET per page; paid plans add link-page GETs for every issue |
| Get | One issue GET; paid plans also fetch its link pages |
| Create | One issue POST, optional close PUT, estimate POST, dependency operations and final Get; username lookups add one user-list GET per assignee/page |
| Update/move | Two Gets (before and after), at most one issue PUT, optional estimate/reset POST and dependency operations; username lookups as above |
| Dependency edits | Read link pages; one DELETE per removed blocker and one POST per new blocker; existing unrelated links stay untouched |
| Delete | One permanent issue DELETE |
| Init | Read board pages unless ID supplied; optionally POST board and label lists; look up labels per requested workflow value and POST missing ones; resolve schema before and after provisioning |

The service also reads before editing (one extra Get) and moving (two extra
Gets). This preserves the existing claim/vocabulary pipeline; it is not a
transaction. Existing priority/class labels and adopted board lists are never
rewritten by init. New priority labels receive native numeric ranks from the
requested ordering. Unranked adopted labels sort by name, without a guessed
meaning for labels such as P0/P1.

Board and list reads use the existing decorator. Get and writes stay live.
Multi-call operations are not transactions: identify an already-created issue
in a partial-failure error, and never blindly retry its POST.

## Port findings and generic changes required by #9

The existing `init` assumed GitHub owner/repository/project-number settings
and github.com remotes. The implementation adds generic `init --set key=value`
provider settings and remote parsing for HTTPS/SSH with nested namespaces.
It also removes default claim requirements when the resolved backend cannot
store claims. These are explicit CLI port findings, with no GitLab-specific
condition in the CLI.

The service previously stamped Started/Completed unconditionally. Ticket #8
adds the generic `CapWorkflowDates` capability: automatic stamps are only
written when supported, while explicit unsupported date edits fail with a
reason. GitLab reads `closed_at` after close/reopen. No fields were added to
Task, Draft or Patch; GitLab-specific state stays in Metadata.

## Live evidence and remaining coverage

Captured full responses and pagination headers are replayed by the adapter tests.
The sandbox remains available for repeatable manual validation; tests never use
its credentials or contact it. Evidence captured:

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
- Native priority label ranks were updated and read back on Free.
- Namespace plan discovery succeeded for the authorized Owner account.
- The glab credential helper returned the existing keyring login and expiry;
  glab auth status renewed an expired OAuth token without a new user login.

The actual yakanban binary also passed init/adoption, config, create, show,
list, board, edit/append, move/close/reopen, estimate/due/assignee clearing,
unsupported dependency/claim/workflow-date requests, and permanent deletion
followed by not-found. The smoke issue was removed after validation.

Remaining live coverage: paid dependency writes, paid scope exclusivity,
self-managed authentication, non-Owner plan visibility, and multi-assignee
license behavior. These must not be described as live-verified. The adapter
refuses unavailable or unknown capabilities with the precise known cause.
