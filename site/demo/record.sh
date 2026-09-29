#!/usr/bin/env bash
# Records one take of the hero clip: a real Claude Code session driving
# yakanban on aramponi/acme-api, with the board sampled beside it.
#
#   site/demo/record.sh [OUT_DIR]
#
# Every take starts from the same place: the repository is reset to
# site/demo/seed, its open issues are closed and the boards earlier takes
# created are closed (not deleted), so the session's `yakanban init` creates a
# fresh one. The take is written to OUT_DIR (default site/demo/takes/<UTC time>):
#
#   session.tsv  <epoch ms> TAB <one stream-json event>, all three prompts
#   board.tsv    <epoch ms> TAB <the board as JSON, bodies stripped>
#   meta.json    versions, prompts, seed commit, session id
#
# It costs real tokens and makes real writes to aramponi/acme-api.
set -euo pipefail

OWNER=aramponi
REPO=acme-api
BOARD_TITLE=acme-api
POLL_SECONDS=3
BUDGET_USD=${BUDGET_USD:-20}   # per prompt

HERE=$(cd "$(dirname "$0")" && pwd)
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
OUT=${1:-$HERE/takes/$STAMP}
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
WORK=$(mktemp -d)
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$WORK"' EXIT

# What the user types. These are shown in the clip verbatim.
PROMPTS=(
  "Set up a yakanban board for this repo."
  "File a ticket for each problem you find: failing tests, the README, the TODOs."
  "Work the board with sub-agents in parallel. Pick the model and the effort for each one to fit its ticket. Then review, commit, push and close them."
)

say() { printf '\033[2m[record]\033[0m %s\n' "$*" >&2; }

# ---------- reset ----------

say "resetting $OWNER/$REPO to the seed"
cp -R "$HERE/seed" "$WORK/seed"
(
  cd "$WORK/seed"
  rm -f SEED.md # notes for this repository, not part of the demo project
  git init -q -b main
  git add -A
  git -c user.name="yakanban demo" -c user.email="demo@invalid" commit -q -m "Initial import"
  git push -q --force "https://github.com/$OWNER/$REPO.git" main
)
SEED_TREE=$(git -C "$WORK/seed" rev-parse HEAD^{tree})

for n in $(gh issue list -R "$OWNER/$REPO" --state open --limit 200 --json number -q '.[].number'); do
  gh issue close -R "$OWNER/$REPO" "$n" --reason "not planned" >/dev/null
done
for n in $(gh project list --owner "$OWNER" --format json --limit 200 \
    -q ".projects[] | select(.title == \"$BOARD_TITLE\" and (.closed | not)) | .number"); do
  say "closing board #$n from an earlier take"
  gh project close "$n" --owner "$OWNER" >/dev/null
done

git clone -q "https://github.com/$OWNER/$REPO.git" "$WORK/$REPO"
cd "$WORK/$REPO"

# ---------- board sampler ----------

sample_board() {
  while :; do
    if [ -f .yakanban.yml ]; then
      if snap=$(yakanban list --json --no-cache 2>/dev/null); then
        printf '%s\t%s\n' "$(python3 -c 'import time; print(int(time.time()*1000))')" \
          "$(printf '%s' "$snap" | python3 -c 'import json,sys; d=json.load(sys.stdin); [t.pop("body",None) for t in d]; print(json.dumps(d,separators=(",",":")))')" \
          >> "$OUT/board.tsv"
      fi
    fi
    sleep "$POLL_SECONDS"
  done
}
sample_board &

# ---------- session ----------

stamp() { python3 -u -c '
import sys, time
for line in sys.stdin:
    sys.stdout.write("%d\t%s" % (time.time() * 1000, line))
    sys.stdout.flush()
'; }

# The session is its own top-level Claude Code, even when this script is run
# from inside one.
unset CLAUDECODE CLAUDE_CODE_ENTRYPOINT

SID=$(uuidgen | tr "[:upper:]" "[:lower:]")
ALLOWED=(Read Edit Write Glob Grep Agent Skill
  "Bash(yakanban:*)" "Bash(go:*)" "Bash(gofmt:*)" "Bash(make:*)" "Bash(git:*)")

for i in "${!PROMPTS[@]}"; do
  say "prompt $((i + 1))/${#PROMPTS[@]}: ${PROMPTS[$i]}"
  if [ "$i" -eq 0 ]; then session=(--session-id "$SID"); else session=(--resume "$SID"); fi
  printf '%s\t%s\n' "$(python3 -c 'import time; print(int(time.time()*1000))')" \
    "$(python3 -c 'import json,sys; print(json.dumps({"type":"demo_prompt","text":sys.argv[1]}))' "${PROMPTS[$i]}")" \
    >> "$OUT/session.tsv"
  claude -p "${PROMPTS[$i]}" "${session[@]}" \
    --model opus \
    --output-format stream-json --verbose \
    --permission-mode acceptEdits \
    --allowedTools "${ALLOWED[@]}" \
    --max-budget-usd "$BUDGET_USD" \
    | stamp >> "$OUT/session.tsv"
done

# One last sample after the session, so the final board is always captured.
sleep "$POLL_SECONDS"

cat > "$OUT/meta.json" <<EOF
{
  "recorded": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "repository": "$OWNER/$REPO",
  "session_id": "$SID",
  "seed_tree": "$SEED_TREE",
  "claude": "$(claude --version | head -1)",
  "yakanban": "$(yakanban --version 2>/dev/null | head -1)",
  "prompts": $(python3 -c 'import json,sys; print(json.dumps(sys.argv[1:]))' "${PROMPTS[@]}")
}
EOF
say "take written to $OUT"
