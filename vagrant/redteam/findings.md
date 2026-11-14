# Red-team findings — triage log

One row per scenario run that produced a verdict; evidence lives under
`evidence/<run-id>/` (gitignored), and each row quotes the relevant bytes
rather than attaching dumps. Outcome taxonomy: **CONFIRMED** (the attack
worked as described), **MITIGATED** (attack degraded, defense held the
line), **BLOCKED** (defense rejected it outright), **NA** (could not
exercise). Scenario expectations live in the scenario scripts; a finding
that gets fixed flips its `EXPECT` in the same PR as the fix
(docs/redteam-implementation.md, conventions §2).

| scenario | date | matrix row | outcome | severity | evidence | issue / follow-up |
|---|---|---|---|---|---|---|
| rt-1-1 | 2026-10-08 | host+off+priv | CONFIRMED | S4 (recon, documented default) | see run dir | expected: the daemon's default row serves /metrics unauthenticated to any container that can reach the docker0 gateway; docs/security.md recommends bearer auth + loopback/UDS in deployment. Scenario pins the exposure; ops recommendation tracked in docs/security.md. |

## Notes

- Severity scale: docs/redteam-plan.md §severity.
- Rows are appended, never edited, so the log doubles as the program's
  execution record.
