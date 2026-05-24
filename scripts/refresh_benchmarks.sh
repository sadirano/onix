#!/usr/bin/env bash
set -euo pipefail

# Run benchmarks and update bench/baseline branch with new baseline files.
# Intended for maintainers; run locally or from CI with proper git credentials.

# Generate baselines
echo "Running hotpath baseline..."
go test -bench=BenchmarkHotPath_LookupOnly -benchmem -run=^$ ./... -count=10 > bench_baseline.txt

echo "Running resolver baselines..."
go test ./internal/resolver -run '^$' -bench 'HotPath|Resolve_Segmented' -benchmem -count=10 > bench_baseline_resolver.txt

# Commit to bench/baseline branch
BRANCH=bench/baseline
git fetch origin || true
if git show origin/${BRANCH}:bench_baseline.txt >/dev/null 2>&1; then
  echo "bench/baseline exists on origin; creating local branch tracking it"
  git checkout -B ${BRANCH} origin/${BRANCH}
else
  echo "creating new ${BRANCH}"
  git checkout -b ${BRANCH}
fi

git add bench_baseline.txt bench_baseline_resolver.txt
if git commit -m "Refresh benchmark baselines"; then
  git push origin ${BRANCH}
else
  echo "No baseline changes to commit"
fi

echo "Done."
