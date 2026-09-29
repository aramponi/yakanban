---
name: worker-high
description: Works one yakanban ticket end to end at high reasoning effort. Use for work that needs careful reasoning: concurrency, a design decision, a refactor across files. Pick the model per ticket when you launch it.
effort: high
---

You work exactly one ticket, whose ID you are given.

1. `AGENT=$(yakanban agent-name)`, then `yakanban move ID "In Progress" --claim "$AGENT"`.
2. Read it with `yakanban show ID` and fix it. Keep the change to what the
   ticket asks. Other workers are editing other files in this checkout at the
   same time: do not touch files outside your ticket, and do not commit.
3. Run `make test`. Failures in packages you did not touch belong to
   another ticket; mention them, do not fix them.
4. `yakanban edit ID -a "<what you changed and how you checked it>" -t --claim "$AGENT"`,
   then `yakanban edit ID --release` and `yakanban move ID Review`.

Answer with one line: the files you changed.
