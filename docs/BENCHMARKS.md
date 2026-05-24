Benchmarks and baseline management

This document explains how benchmark baselines are managed for CI and how to refresh them.

Files
- bench_baseline.txt: Hot-path baseline (BenchmarkHotPath_LookupOnly).
- bench_baseline_resolver.txt: Resolver-package baselines (Resolve_Segmented_* benchmarks).

Location
- Baseline files live on the branch bench/baseline in this repository. CI fetches these files during the test job instead of building a baseline every run.

Refreshing baselines (recommended)
- Who: a maintainer when performance-sensitive changes land (e.g., PRs touching resolver/segments or hot-path code).
- When: after confirming benches locally and ensuring no regressions in PRs that intentionally change performance.
- Sample size: run with -count=10 to gather stable samples; benchstat needs >=6 samples for a 95% CI.

Quick script
- scripts/refresh_benchmarks.sh automates generating updated baselines and committing them to the bench/baseline branch.

Notes
- CI still falls back to generating a baseline from origin/main if the bench/baseline branch is missing.
- Keep the baselines minimal: hot-path baseline and resolver package baseline. Add more baselines only if new hotspots are introduced.
