# stuntshell examples

Two runnable, self-contained agent tests. Both build stuntshell from source,
stage doubles into a temp directory, run a stand-in "agent" against them,
and finish with `stuntshell verify` — offline and deterministic.

## test-git-agent.sh + git-agent.json

The classic shape: an agent is supposed to inspect, stage, commit, and push —
in that order, exactly once each, and never force-push. The manifest turns
each of those sentences into an expectation (`ordered`, `exactly`, `max: 0`),
and `strict` additionally rejects any git invocation nobody anticipated.

```bash
bash examples/test-git-agent.sh
```

Try breaking the agent — add a `git push --force origin main` line or swap
the commit and push — and watch `verify` fail with the exact expectation
that was violated.

## test-retry-loop.sh + retry-loop.json

Scripted takes: the `curl` double answers the same argv differently per
invocation — two connection failures, then success. The retry loop under
test must survive the failures, and the expectation `min: 3, max: 5` proves
it retried without hammering. No server, no network, no sleeps.

```bash
bash examples/test-retry-loop.sh
```

Both scripts print the `verify` report and exit 0 only when every
expectation holds, so they drop straight into any test harness.
