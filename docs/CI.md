CI benchmark gate

This repository enforces a hot-path benchmark gate in CI for BenchmarkHotPath_LookupOnly.

- Gate: the CI job compares the current bench run against a baseline from main using benchstat and fails the job if the change is a slowdown > 20%.

Local reproduction

1. Run the hot-path benchmark and save current runs:
   go test -bench=BenchmarkHotPath_LookupOnly -benchmem -run=^$ -count=10 ./... > bench_current.txt

2. If you don't have a baseline, create one from main locally:
   git fetch origin main
   git worktree add --force baseline origin/main
   pushd baseline; go test -bench=BenchmarkHotPath_LookupOnly -benchmem -run=^$ -count=10 ./... > ..\bench_baseline.txt; popd
   git worktree remove --force baseline

3. Install benchstat and compare:
   go install golang.org/x/perf/cmd/benchstat@latest
   benchstat bench_baseline.txt bench_current.txt

Interpretation

- benchstat prints the % change vs baseline; positive % means slower. CI treats >20% slowdown as failure.
- For stable signals, increase -count or use -benchtime; run on CI-equivalent hardware when possible.

Updating the baseline

If changes improve performance and you want to update the baseline, run the baseline collection against main (as above) and commit bench_baseline.txt to the repo or store it in CI artifacts as appropriate.
