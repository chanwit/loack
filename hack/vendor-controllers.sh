#!/usr/bin/env bash
#
# Vendor the ACK controller clones loack links against, pinned by controllers.lock.
#
# The all-in-one loack-aio binary links every wired controller, so they must all
# resolve the same ACK runtime version. This script enforces that: after
# cloning/checking out the pinned commits it verifies every clone's go.mod uses
# RUNTIME, and fails loudly otherwise so the dependency graph can't drift apart.
#
# Provider modules (providers/*/) are their own Go modules that link a SUBSET of
# the same controller clones via their own `replace` directives, and pin their
# own runtime. Verify checks each provider module: every controller it replaces
# must be pinned here (so `make vendor` clones it). A provider may deliberately
# pin a DIFFERENT runtime than the core or than the one a controller was
# generated against -- that independence is the whole point -- so verify reports
# each provider's effective runtime rather than forcing it to match.
#
# Usage:
#   hack/vendor-controllers.sh                clone/checkout pinned commits + verify
#   hack/vendor-controllers.sh --verify-only  verify existing clones, change nothing
#   hack/vendor-controllers.sh --relock       re-pin controllers.lock to current HEADs
#
set -euo pipefail

ORG=https://github.com/aws-controllers-k8s
LOCK="controllers.lock"
MODE="vendor"
case "${1:-}" in
  --verify-only) MODE="verify" ;;
  --relock)      MODE="relock" ;;
  "")            ;;
  *) echo "unknown argument: $1" >&2; exit 2 ;;
esac

[ -f "$LOCK" ] || { echo "error: $LOCK not found (run from the repo root)" >&2; exit 1; }

# Parse the lock file into RUNTIME and a list of "<name> <sha>" entries.
RUNTIME=""
NAMES=()
SHAS=()
while read -r col1 col2 _; do
  case "$col1" in
    ""|\#*) continue ;;
    RUNTIME) RUNTIME="$col2" ;;
    *)       NAMES+=("$col1"); SHAS+=("$col2") ;;
  esac
done < "$LOCK"

[ -n "$RUNTIME" ] || { echo "error: no RUNTIME pinned in $LOCK" >&2; exit 1; }

runtime_of() { # $1=dir -> prints the pinned runtime version, or nothing
  # Print the version token (vX.Y.Z) on the runtime line, so it works for both
  # block `   github.com/.../runtime v1 // indirect` and `require ... v1` forms.
  awk '/aws-controllers-k8s\/runtime[ \t]/{for(i=1;i<=NF;i++) if($i ~ /^v[0-9]/){print $i; exit}}' "$1/go.mod" 2>/dev/null
}

# prov_controllers $1=go.mod -> controller clone dir names this module replaces.
# Matches `replace .../<svc>-controller => <path>/<svc>-controller` and prints
# the basename of the replacement path (the clone dir, e.g. s3-controller).
prov_controllers() {
  grep -E '=>[[:space:]]+\.\.' "$1" \
    | grep -E 'aws-controllers-k8s/[a-z0-9-]+-controller' \
    | sed -E 's#.*=>[[:space:]]+##' | awk '{print $1}' \
    | sed -E 's#.*/##' | sort -u
}

# --- relock: rewrite the lock from the current clones -----------------------
if [ "$MODE" = "relock" ]; then
  common=""
  for name in "${NAMES[@]}"; do
    [ -d "$name/.git" ] || { echo "error: $name not cloned; run 'make vendor' first" >&2; exit 1; }
    rt="$(runtime_of "$name")"
    if [ -z "$common" ]; then common="$rt"; fi
    if [ "$rt" != "$common" ]; then
      echo "error: runtime mismatch -- $name uses $rt but $common is expected" >&2
      echo "       refusing to relock a divergent set." >&2
      exit 1
    fi
  done
  {
    awk '/^RUNTIME /{print "RUNTIME '"$common"'"; next} /^[a-z0-9-]+-controller /{next} {print}' "$LOCK"
    echo
    for name in "${NAMES[@]}"; do
      printf "%-25s %s\n" "$name" "$(git -C "$name" rev-parse HEAD)"
    done
  } > "$LOCK.tmp"
  mv "$LOCK.tmp" "$LOCK"
  echo "Re-pinned $LOCK to current HEADs (runtime $common)."
  exit 0
fi

# --- vendor: clone/checkout the pinned commits ------------------------------
if [ "$MODE" = "vendor" ]; then
  for i in "${!NAMES[@]}"; do
    name="${NAMES[$i]}"; sha="${SHAS[$i]}"
    fresh=0
    if [ ! -d "$name/.git" ]; then
      echo "cloning $name"
      git clone --quiet --depth 1 "$ORG/$name.git" "$name"
      fresh=1
    fi
    if [ "$fresh" -eq 1 ] || [ "$(git -C "$name" rev-parse HEAD 2>/dev/null)" != "$sha" ]; then
      # Fetch the exact pin (GitHub allows fetch-by-sha) and check it out so the
      # working tree -- including go.mod -- is materialized at the pinned commit.
      git -C "$name" fetch --quiet --depth 1 origin "$sha"
      git -C "$name" checkout --quiet --detach "$sha"
      echo "checked out $name @ ${sha:0:12}"
    else
      echo "ok $name @ ${sha:0:12}"
    fi
  done
fi

# --- verify -----------------------------------------------------------------
fail=0

in_lock() { local n="$1" x; for x in "${NAMES[@]}"; do [ "$x" = "$n" ] && return 0; done; return 1; }

# Cross-check the lock against go.mod: every ACK controller wired via a `replace`
# directive must be pinned here, and every pinned controller must be wired.
# This stops a controller from being added to the build without a pin (or left
# pinned after it's removed), which is how the shared dependency set drifts.
REPLACED="$(grep -E '=>[[:space:]]+\./' go.mod \
  | grep -E 'aws-controllers-k8s/[a-z0-9-]+-controller' \
  | sed -E 's#.*=>[[:space:]]+\./##' | awk '{print $1}' | sort -u)"

for n in $REPLACED; do
  in_lock "$n" || { echo "UNPINNED $n: replaced in go.mod but missing from $LOCK" >&2; fail=1; }
done
for n in "${NAMES[@]}"; do
  printf '%s\n' $REPLACED | grep -qx "$n" || {
    echo "UNWIRED  $n: pinned in $LOCK but has no replace in go.mod" >&2; fail=1; }
done

# Every clone is at its pin and shares RUNTIME.
for i in "${!NAMES[@]}"; do
  name="${NAMES[$i]}"; sha="${SHAS[$i]}"
  if [ ! -d "$name/.git" ]; then
    echo "MISSING  $name (run 'make vendor')" >&2; fail=1; continue
  fi
  head="$(git -C "$name" rev-parse HEAD)"
  rt="$(runtime_of "$name")"
  if [ "$head" != "$sha" ]; then
    echo "UNPINNED $name: at ${head:0:12}, want ${sha:0:12}" >&2; fail=1
  fi
  if [ "$rt" != "$RUNTIME" ]; then
    echo "RUNTIME  $name: uses $rt, want $RUNTIME" >&2; fail=1
  fi
done
# Provider modules: each is its own module linking a subset of the same clones.
# Every controller a provider replaces must be pinned here (else `make vendor`
# won't clone it). A provider's effective runtime (a `replace ... runtime => ...`
# wins over its require line) may legitimately differ from the clone's -- we
# report it, not enforce it.
shopt -s nullglob
nprov=0
for pgomod in providers/*/go.mod; do
  nprov=$((nprov + 1))
  pdir="$(dirname "$pgomod")"
  pmod="$(awk '/^module /{print $2; exit}' "$pgomod")"
  rrepl="$(grep -E 'replace.*aws-controllers-k8s/runtime.*=>' "$pgomod" | grep -oE 'v[0-9][^ ]*' | tail -1 || true)"
  prt="${rrepl:-$(runtime_of "$pdir")}"
  if [ -z "$prt" ]; then
    echo "RUNTIME  $pmod: no aws-controllers-k8s/runtime pinned in $pgomod" >&2; fail=1
  fi
  cs=""
  for c in $(prov_controllers "$pgomod"); do
    cs="$cs $c"
    in_lock "$c" || { echo "UNVENDORED $c: replaced by $pmod but not pinned in $LOCK" >&2; fail=1; }
  done
  echo "  provider $pmod: runtime ${prt:-?} (controllers:${cs:- none})"
done

if [ "$fail" -ne 0 ]; then
  echo "vendor verification FAILED" >&2
  exit 1
fi
echo "All ${#NAMES[@]} controllers pinned and on runtime $RUNTIME; ${nprov} provider module(s) consistent."
