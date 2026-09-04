# GitHub mapping

yakanban splits a task across two GitHub objects:

- the **issue** holds the content people read and comment on;
- the **project item** holds the workflow state the board is about.

Both stay perfectly usable in the web UI. Someone who has never heard of
yakanban can drag a card between columns, set its priority in the project
table, and the CLI will report it on the next command.

## Field table

| Domain field | GitHub | Notes |
|---|---|---|
| `ID` | issue number | `yakanban show 42` ↔ issue #42 |
| `Title`, `Body` | issue title, body | REST |
| `Tags` | issue labels | `--add-tag` / `--remove-tag` merge, they do not replace |
| `Assignees` | issue assignees | `--assignee` replaces the list |
| `URL` | issue URL | |
| `Created`, `Updated` | issue timestamps | read-only |
| `Status` | project `Status` single-select | the built-in column set |
| `Priority` | project `Priority` single-select | created by `init` |
| `Class` | project `Class` single-select | class of service |
| `Estimate` | project `Estimate` text | free-form, e.g. `4h` |
| `Due`, `Started`, `Completed` | project date fields | `Started`/`Completed` are stamped on transitions |
| `Blocked` | project `Blocked` text | a non-empty reason means blocked |
| `DependsOn` | project `Depends On` text | comma-separated issue numbers |
| `Parent` | project `Parent` text | issue number |
| `Claim` | project `Claim` text | agent name |
| — | project `Claim Expires` text | RFC 3339; project date fields have no time component, hence text |
| `Metadata` | — | project item ID, issue state, archived flag |

## What `yakanban init` does

With `--project N` it adopts the project as it is: **the existing `Status`
columns are never rewritten**. It only creates the custom fields that are
missing.

Without `--project`, it creates a Projects v2 board, links it to the
repository, and sets the columns from the descriptor (safe: the project is
empty at that point).

Either way, existing data is never deleted. If you removed a yakanban field by
hand, `yakanban init --force` recreates it.

## Fixtures

`internal/provider/github/testdata/live_*.json` are real API responses,
captured with the queries in `queries.go` against a throwaway project. They
pin two things the documentation cannot: that a fully-populated item already
returns fifteen field values (hence `fieldValues(first:100)` — truncation
would silently blank a status or a claim), and that GitHub interleaves value
types yakanban does not model (`ProjectV2ItemFieldUserValue`,
`…RepositoryValue`, `…LabelValue`), which the decoder must skip rather than
choke on.

## API traffic

| Command | Calls |
|---|---|
| `list`, `board` | 1 GraphQL for the project schema (cached on disk) + 1 per 100 items — then nothing at all until the TTL expires |
| `show` | 1 GraphQL |
| `create` | 1 REST + 1 GraphQL to add the item + 1 GraphQL per field set |
| `edit`, `move` | 1 GraphQL read + at most 1 REST + 1 GraphQL per changed field |

Projects v2 has no bulk field mutation, which is why a write costs one round
trip per changed field.

## Limits

- **Draft issues and pull requests** on the board are skipped: yakanban only
  manages real issues.
- **Archived items** are hidden from `list`.
- **`delete` closes and archives.** GitHub only lets a repository admin truly
  delete an issue, and doing so would take its whole discussion with it.
- **One repository per board.** A project spanning several repositories works
  for reads, but `create` opens issues in the configured repository.
- **`Claim Expires` is a text field** because a Projects v2 date field has no
  time component and a one-hour claim needs one.
