# Captured GitLab responses

These are complete JSON response bodies captured from the authorized disposable
`aramponi/mysandbox` project (86114941), board 11585676, on gitlab.com on
2026-09-04 UTC / 2026-09-05 Europe/Paris. GitLab reported `19.4.0-pre`, namespace
plan `free`, and caller project access level 50 (Owner).

Each envelope records the capture time, request method/path/body, HTTP status,
pagination headers and response body. Transport telemetry headers were omitted;
fields whose names contain `token` were replaced with `REDACTED`. No API token
or OAuth credentials are included. The issue text was created for these tests.

`project` and `namespace` are the same objects fetched by the adapter using
project path / namespace ID aliases. `issues_page1` and `issues_page2` deliberately
use a page size of one to capture real pagination. Tests replay those pages even
when the production client requests a larger page size.

`dependency_refused` is an actual Free 403 response. `related_created` and `links`
record the supported undirected relation. Paid-plan branches in tests are
explicitly synthetic variants of these captures, not claimed Premium captures.
Null/partial-success fault injection is also explicitly synthetic.

No test accesses the network beyond its local httptest server. Live smoke tests
are manual and must only run on a disposable, explicitly authorized project.
