#!/usr/bin/env bash
#
# Prove that contract-guard.sh can fail.
#
# A gate is only worth running if it can report the negative. This builds a
# small tree for each way the registry and the tests can drift apart, runs the
# guard against it with --root, and checks the exit code. A guard that passes
# everything and a guard that works look identical from the outside, and this is
# the difference.
#
# Usage: scripts/contract-guard-selftest.sh
# Exit: 0 every case behaved, 1 a case did not.

set -uo pipefail

GUARD="$(cd "$(dirname "$0")" && pwd)/contract-guard.sh"
FAILURES=0

# fixture DIR builds a healthy tree: one contract, one tagged test that the
# registry names.
fixture() {
  local d="$1"
  mkdir -p "$d/docs" "$d/pkg/fake"
  # Two contracts, and the ids are chosen so their order differs between
  # collations: under C the hyphen (0x2D) puts CC1- before CC10, and under a
  # UTF-8 locale punctuation is weighted below letters and the order reverses.
  # A one-contract fixture cannot exhibit the defect CI found.
  cat > "$d/docs/CONSUMER_CONTRACTS.md" <<'EOF'
# Consumer contracts

| id | Invariant | Consumer(s) | Guarding test(s) | Origin |
|----|-----------|-------------|------------------|--------|
| CC1-fixture | A fixture invariant | fixture-consumer | `pkg/fake` `TestFixture` | #1 |
| CC10-fixture-ten | A second fixture invariant | fixture-consumer | `pkg/fake` `TestFixtureTen` | #10 |
EOF
  cat > "$d/pkg/fake/fixture_test.go" <<'EOF'
package fake

import "testing"

// CONSUMER CONTRACT: CC1-fixture — fixture-consumer (#1)
func TestFixture(t *testing.T) {
	if 1 != 1 {
		t.Fatal("arithmetic")
	}
}

// CONSUMER CONTRACT: CC10-fixture-ten — fixture-consumer (#10)
func TestFixtureTen(t *testing.T) {
	if 2 != 2 {
		t.Fatal("arithmetic")
	}
}
EOF
}

# expect NAME EXPECTED_EXIT DIR
expect() {
  local name="$1" want="$2" dir="$3"
  bash "$GUARD" --root "$dir" >/dev/null 2>&1
  local got=$?
  if [ "$got" = "$want" ]; then
    echo "ok    $name (exit $got)"
  else
    echo "FAIL  $name: exit $got, wanted $want"
    FAILURES=$((FAILURES + 1))
  fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# 0. A healthy tree passes, once its lock exists. If this case fails, every
#    other case below could be passing for the wrong reason.
fixture "$WORK/healthy"
bash "$GUARD" --root "$WORK/healthy" --update >/dev/null 2>&1
expect "healthy tree passes" 0 "$WORK/healthy"

# 1. A registry row that no test is tagged with.
fixture "$WORK/orphan-row"
bash "$GUARD" --root "$WORK/orphan-row" --update >/dev/null 2>&1
sed -i.bak 's|// CONSUMER CONTRACT: CC1-fixture .*|// a plain comment|' "$WORK/orphan-row/pkg/fake/fixture_test.go"
expect "registry row with no tagged test" 1 "$WORK/orphan-row"

# 2. A tagged test with no registry row.
fixture "$WORK/orphan-tag"
bash "$GUARD" --root "$WORK/orphan-tag" --update >/dev/null 2>&1
sed -i.bak 's|CC1-fixture — fixture-consumer|CC99-unregistered — fixture-consumer|' "$WORK/orphan-tag/pkg/fake/fixture_test.go"
expect "tagged test with no registry row" 1 "$WORK/orphan-tag"

# 3. A contract comment with no id at all, which is what the real tree had.
fixture "$WORK/idless"
bash "$GUARD" --root "$WORK/idless" --update >/dev/null 2>&1
printf '\n// CONSUMER CONTRACT: this one relies on prose\nfunc TestOther(t *testing.T) {\n}\n' >> "$WORK/idless/pkg/fake/fixture_test.go"
expect "contract comment with no id" 1 "$WORK/idless"

# 4. The registry names a test that does not exist.
fixture "$WORK/missing-test"
bash "$GUARD" --root "$WORK/missing-test" --update >/dev/null 2>&1
sed -i.bak 's|`TestFixture`|`TestRenamedAway`|' "$WORK/missing-test/docs/CONSUMER_CONTRACTS.md"
expect "registry names a test that does not exist" 1 "$WORK/missing-test"

# 5. The test still exists and still has its tag, but its body changed. This is
#    the case the lock file exists for: an assertion weakened in place.
fixture "$WORK/weakened"
bash "$GUARD" --root "$WORK/weakened" --update >/dev/null 2>&1
sed -i.bak 's|t.Fatal("arithmetic")|t.Skip("flaky")|' "$WORK/weakened/pkg/fake/fixture_test.go"
expect "guarding test body changed after the lock" 1 "$WORK/weakened"

# 6. The lock file is missing.
fixture "$WORK/nolock"
expect "lock file missing" 1 "$WORK/nolock"

# 7. No registry at all. This must be exit 2, not exit 0: a check that cannot
#    find its input has read nothing.
mkdir -p "$WORK/noregistry/pkg/fake"
expect "registry missing" 2 "$WORK/noregistry"

# 8. A registry with rows, test files present, and every tag removed. The rows
#    are unenforced, which is a violation rather than a broken instrument.
fixture "$WORK/notags"
sed -i.bak 's|// CONSUMER CONTRACT: .*|// nothing here|' "$WORK/notags/pkg/fake/fixture_test.go"
expect "every tag removed but tests present" 1 "$WORK/notags"

# 9. No tags and no test files: the check is looking at the wrong tree, so it
#    has read nothing and must say so instead of passing.
fixture "$WORK/wrongtree"
rm "$WORK/wrongtree/pkg/fake/fixture_test.go"
expect "no tags and no test files" 2 "$WORK/wrongtree"

# 10. The lock must be in byte order, whatever locale the caller has. This is
#     the defect CI found on the first run of this gate: the digests matched
#     exactly, only the order differed, and a lock written on a developer
#     machine failed on the runner.
#
#     Two checks, because either alone is weak. The byte-order assertion always
#     runs. The locale comparison only runs where a UTF-8 locale is installed,
#     and it is skipped rather than silently passing where one is not — a
#     comparison between two identical fallbacks proves nothing.
fixture "$WORK/locale"
bash "$GUARD" --root "$WORK/locale" --update >/dev/null 2>&1
LOCK="$WORK/locale/docs/consumer-contracts.lock"

if grep -v '^#' "$LOCK" | grep -v '^$' | LC_ALL=C sort -c 2>/dev/null; then
  echo "ok    the lock is in byte order"
else
  echo "FAIL  the lock is not in byte order, so it depends on the writer's collation"
  FAILURES=$((FAILURES + 1))
fi

UTF8_LOCALE="$(locale -a 2>/dev/null | grep -iE '^(en_[A-Z]{2}\.)(utf-?8)$' | head -1)"
if [ -z "$UTF8_LOCALE" ]; then
  echo "skip  no UTF-8 locale installed, so the collation comparison cannot run"
else
  # Both ends are named. Comparing the ambient locale against a UTF-8 one
  # passes for free wherever the ambient locale is already UTF-8, which is
  # most developer machines, and that is a case that proves nothing.
  LC_ALL=C bash "$GUARD" --root "$WORK/locale" --update >/dev/null 2>&1
  cp "$LOCK" "$WORK/locale-c.lock"
  LC_ALL="$UTF8_LOCALE" bash "$GUARD" --root "$WORK/locale" --update >/dev/null 2>&1
  if cmp -s "$WORK/locale-c.lock" "$LOCK"; then
    echo "ok    the lock is the same under C and $UTF8_LOCALE"
  else
    echo "FAIL  the lock differs between C and $UTF8_LOCALE"
    FAILURES=$((FAILURES + 1))
  fi
fi

echo
if [ "$FAILURES" = "0" ]; then
  echo "contract-guard-selftest: all 11 cases behaved"
  exit 0
fi
echo "contract-guard-selftest: $FAILURES case(s) did not behave"
exit 1
