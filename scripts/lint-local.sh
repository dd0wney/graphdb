#!/usr/bin/env bash
#
# Run as much of the CI lint as this machine can.
#
# `golangci-lint run ./...` does not work here. It fails to typecheck a Go 1.26
# standard library file:
#
#   /usr/local/go/src/internal/poll/splice_linux.go:237:21: unknown field rfd
#   in struct literal of type splicePipe (typecheck)
#
# A typecheck failure suppresses the analysers, so the command reports one
# environment error and nothing at all about the code. CLAUDE.md recorded that
# as "CI's lint is the only working linter", and two pull requests were sent to
# CI to find out what a local run should have said.
#
# That was too pessimistic. Only govet and staticcheck trip it. The other nine
# linters CI runs work here, with CI's own settings, and they catch real
# findings — scripts/lint-local-selftest.sh proves it on a fixture.
#
# Usage: scripts/lint-local.sh [packages...]     (default ./pkg/... ./cmd/...)
#
# Exit codes:
#   0  no findings from the nine linters this can run
#   1  findings
#   2  the check could not run
#
# THIS IS NOT THE GATE. govet and staticcheck are not covered, and CI decides.
# It is here to catch the findings that do not need a round trip to find.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONFIG="$ROOT/.golangci.yml"

if [ ! -s "$CONFIG" ]; then
  echo "lint-local: '$CONFIG' is missing or empty — refusing to report a pass" >&2
  exit 2
fi
if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "lint-local: golangci-lint is not installed — refusing to report a pass" >&2
  exit 2
fi

# The generated config is CI's, with two changes: the default linter set is
# turned off and named explicitly instead, so govet and staticcheck can be left
# out. Everything else — the settings, the exclusion presets, the per-path
# rules — is CI's file, read at run time, so the two cannot drift apart.
GENERATED="$(mktemp -t lint-local-XXXXXX.yml)"
trap 'rm -f "$GENERATED"' EXIT

python3 - "$CONFIG" "$GENERATED" <<'PY'
import sys, pathlib

src = pathlib.Path(sys.argv[1]).read_text()
marker = "linters:\n  enable:\n"
if marker not in src:
    sys.stderr.write("lint-local: could not find the linters.enable block in the config\n")
    sys.exit(2)

# golangci-lint v2 enables errcheck, govet, ineffassign, staticcheck and unused
# by default. Naming them here and setting default:none keeps all of them
# except the two that cannot run.
replacement = (
    "linters:\n"
    "  default: none\n"
    "  enable:\n"
    "    - errcheck\n"
    "    - ineffassign\n"
    "    - unused\n"
)
pathlib.Path(sys.argv[2]).write_text(src.replace(marker, replacement, 1))
PY
if [ $? -ne 0 ]; then
  exit 2
fi

if [ "$#" -eq 0 ]; then
  set -- ./pkg/... ./cmd/...
fi

echo "lint-local: nine of CI's eleven linters. govet and staticcheck cannot run here."

OUTPUT="$(golangci-lint run -c "$GENERATED" "$@" 2>&1)"
status=$?
printf '%s\n' "$OUTPUT"

if [ "$status" -eq 0 ]; then
  echo "lint-local: no findings. This is not a green CI: govet and staticcheck were not run."
  exit 0
fi

# A typecheck finding is not a finding about the code. It means golangci-lint
# could not load the package, so the analysers did not run on it at all, and
# reporting that as "the lint failed" is as wrong as reporting it as a pass.
#
# It happens here for reasons that are nothing to do with the code:
# pkg/storage imports math/rand/v2, which this golangci-lint cannot resolve,
# while go vet and go build are clean on the same file.
if printf '%s' "$OUTPUT" | grep -qE '^\* typecheck: [0-9]+$'; then
  if ! printf '%s' "$OUTPUT" | grep -qE '^\* (errcheck|ineffassign|unused|gocritic|gosec|misspell|nakedret|revive|unconvert): '; then
    echo "lint-local: every finding above is a typecheck error, which means the analysers" >&2
    echo "lint-local: did not run on those packages. This is not a result about the code." >&2
    echo "lint-local: check 'go build' and 'go vet' on them, and let CI decide the lint." >&2
    exit 2
  fi
  echo "lint-local: some packages failed to load (typecheck above) and were NOT analysed." >&2
fi

exit "$status"
