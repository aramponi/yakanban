# Adopting a board that already exists

Nothing about yakanban assumes a fresh repository. `init` provisions a board and
writes a descriptor; it never touches your code, your issues or your history.

```bash
gh auth refresh -s project          # once, if you authenticated before Projects v2
yakanban init --branching github-flow
```

`owner` and `repo` come from `origin`, so this is the whole of it: a Project v2
is created, linked to the repository and given the default columns.

## Adopting the team's project

If the team already has a board, adopt it instead of adding a second one:

```bash
yakanban init --project 7
```

The project is taken as it is. **Its `Status` columns are never rewritten** —
yakanban only creates the custom fields it is missing (Priority, Class, Claim,
Blocked, Depends On…), and deletes nothing. `init` then writes the columns it
found into `.yakanban.yml`, so the descriptor describes your board rather than
the default one.

## Your old issues are left alone

Issues you already have are **not** bulk-imported, and nothing is migrated
behind your back. An issue that is not on the board is still readable, and the
first write puts it there:

```bash
yakanban show 42                    # works; the task is simply not on the board yet
yakanban move 42 todo               # adds it to the project, then sets the column
```

So a long-lived repository joins the board one ticket at a time, as work
actually reaches it. To bring everything over in one go, select the issues in
the GitHub project's own UI and add them there — yakanban reads the result on
the next command.

## GitLab

The same shape works for GitLab, where `board_id` is the existing board:

```bash
# Inside a GitLab checkout, detect the host and nested namespace from origin:
yakanban init --provider gitlab --branching trunk-based

# Or select the project and optionally an existing board explicitly:
yakanban init --provider gitlab --set project=group/subgroup/repo \
  --set board_id=123 --branching trunk-based
```

GitLab owns the columns: Open, the board's ordered label lists, and Closed.
Free supports ordinary ticket workflows; directional dependencies require
Premium/Ultimate and visible entitlement. Claims, blocked-reason storage,
parent hierarchy and linked branches are explicitly unsupported in this
adapter. GitLab `delete --yes` permanently deletes an issue and requires
Maintainer/Owner access. See
[the mapping and validation limits](gitlab-mapping.md).

Self-managed hosts are configured with `--set host=gitlab.example.com` during
initialization.

## Two things to know afterwards

Columns belong to the tracker: add or rename one in the UI and yakanban
follows, as [configuration](configuration.md#changing-the-columns) describes.

And `init --force` re-provisions the backend from the defaults — it is how you
re-apply a descriptor you have edited, never how you pick up a new column.
