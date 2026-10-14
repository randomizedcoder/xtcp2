# Investigations

Each document here records one production problem: the evidence, the hypotheses and how each was ruled in or out, the experiments that reproduce it, and the fix. An investigation stays here after it's resolved, as the record of why the fix looks the way it does.

| Investigation | Status | Summary |
|---|---|---|
| [Records dropped after the poll-end flush](envelope-post-flush-drop.md) | open: root cause confirmed, fix by TDD | ~19% of collected records in prod are discarded by `Deserialize/envelopePostFlushDrop`, worse on hosts with more namespaces |

## Conventions

- **Name the file after the symptom** (the counter or the behavior), not the suspected cause; the cause can change during the investigation.
- **Quote the evidence with its query and time window**, so it can be rerun. Fleet queries go through Grafana's datasource proxy; see [summary metrics](../design-summary-metrics.md) for the metric layout.
- **Keep the status line current**: `open: <phase>`, `fixed in #<PR>`, or `closed: <reason>`.
