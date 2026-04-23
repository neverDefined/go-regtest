---
description: Run the coverage suite and report exported symbols that are uncovered or weakly covered.
allowed-tools: Read, Bash, Grep
---

Run `make test-coverage` and produce a focused report on coverage gaps for **exported** symbols.

## Steps

1. `ulimit -n 4096 && make test-coverage` (this writes `coverage.out`).
2. Parse `go tool cover -func=coverage.out` to find:
   - Symbols with **0%** coverage.
   - Symbols below **70%** coverage that are public (start with a capital letter and aren't on an unexported type).
3. Group findings by file. For each gap, show: `file.go:line — Symbol — XX.X%`.
4. Note the overall percentage. Compare against the 70% gate (`make test-coverage-check`) — flag if we're below.
5. Suggest test additions for the top 3 gaps, keyed to the `regtest-test-writer` agent's conventions.

## Output style

- Lead with the headline number (e.g., `Total: 81.1% — gate met`).
- Bulleted gap list, capped at ~20 entries.
- 2–3 sentence "next steps" suggestion.

Don't write any tests yourself — this command is read-only. If the user wants to fill the gaps, they (or the `regtest-test-writer` subagent) will follow up.
