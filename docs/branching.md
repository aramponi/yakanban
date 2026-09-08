# Branches and branching models

`yakanban branch` creates a branch on the backend and attaches it to the
ticket — GitHub's linked branches, shown in an issue's Development section. The
work becomes visible to people who have never heard of yakanban, and the pull
request closes the issue on merge without a `closes #N` keyword.

```bash
BRANCH=$(yakanban branch 42 --claim "$AGENT")
git worktree add ../app-task-42 -b "$BRANCH" origin/main
```

The name is decided locally, from a template, and handed to the backend — never
read back from it. That is what lets an agent create its local branch straight
away, from a commit it already has, with no fetch in between.

## Models

`yakanban init` asks which model the repository follows, and writes the answer
to the descriptor. `--branching <model>` skips the question; a piped or CI run
never prompts.

| Model | base, integration | branch |
|---|---|---|
| `trunk-based` | `main`, `main` | `{{.ID}}-{{.Slug}}` |
| `github-flow` | `main`, `main` | `{{.ID}}-{{.Slug}}` |
| `git-flow` | `develop`, `develop` | `{{.Type}}/{{.ID}}-{{.Slug}}` |
| `gitlab-flow` | `main`, `main` | `{{.ID}}-{{.Slug}}` |
| `oneflow` | `main`, `main` | `{{.Type}}/{{.ID}}-{{.Slug}}` |

`model` picks a preset and every key beside it is an override, so `custom`
is simply "no preset":

```yaml
branching:
  model: git-flow
  base: develop           # where work starts
  integration: develop    # where it merges back
  templates:
    branch: "{{.Type}}/{{.ID}}-{{.Slug}}"
    worktree: "../{{.Repo}}-task-{{.ID}}"
  types:
    default: feature
    match:                # first match wins
      - priority: critical
        type: hotfix
        base: main        # a hotfix branches off production
      - tag: bug
        type: fix
```

Templates can use `.ID`, `.Slug`, `.Title`, `.Priority`, `.Class`, `.Type`,
`.Agent`, `.Board` and `.Repo`. `yakanban config` prints the resolved model and
its rules; `yakanban branch ID --dry-run --json` prints the names a given task
would get without creating anything.

## What is deliberately not automated

Release branches and git flow's hotfix back-merge are **not** automated: that is
release engineering, and a silently skipped back-merge loses a fix. yakanban
says the back-merge is yours to do and stops there.

The base commit defaults to the upstream of the current branch and is always
resolved through the **remote**: a commit you have not pushed does not exist
for the backend, and the API error for that says nothing useful.

Branches are an optional capability. A provider that has no such notion reports
it rather than silently doing nothing.
