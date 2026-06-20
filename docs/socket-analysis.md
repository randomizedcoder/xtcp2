# Analyzing socket data: RTT bands and clustering

This guide is for a **data / analytics team** turning xtcp2's per-socket TCP telemetry into insight. It assumes you're comfortable with SQL and basic statistics / clustering. The headline use case is discovering the natural **RTT bands** in your fleet — and doing it *statistically*, because the bands differ by data center and **drift over time**, so hardcoded thresholds go stale. It also sketches other groupings (throughput, retransmission/loss, congestion algorithm, per-ASN, time-of-day).

Read [parquet-format.md](parquet-format.md) first for how to load the data and what the columns mean; this doc is the analysis companion.

## Table of contents

- [The RTT-band mental model](#the-rtt-band-mental-model)
- [Pick the right signal](#pick-the-right-signal)
- [Data preparation](#data-preparation)
- [Finding RTT bands](#finding-rtt-bands)
- [Multi-feature clustering](#multi-feature-clustering)
- [Other useful analyses](#other-useful-analyses)
- [Worked example](#worked-example)
- [Pitfalls](#pitfalls)
- [See also](#see-also)

## The RTT-band mental model

Round-trip time is the strongest single signal for *where* a socket's peer is and *what kind* of path it's on. In a typical fleet you'd expect RTT to fall into a handful of bands, roughly:

1. **Intra-data-center** — peers in the same DC; sub-millisecond to a few ms, usually high throughput.
2. **Metro / same-region CDN edge** — Cloudflare, Fastly, Google, Akamai POPs in the same metro; ~10–30 ms.
3. **Regional services** — another region or a more distant POP; ~60–120 ms.
4. **Outliers** — cross-continent, satellite, or pathological paths; much higher.
5. **Mobile / last-mile** — end users on 4G/LTE/Wi-Fi reaching your hosts; often >150 ms with high variance.

Treat these as **hypotheses, not constants.** The actual band count, centroids, and boundaries vary by DC and move over time (new POPs, peering/routing changes, congestion). The goal is to let the data define the bands and to re-derive them on a schedule, comparing across DCs.

## Pick the right signal

Use **`tcp_info_min_rtt`** as the primary banding feature, not the smoothed `tcp_info_rtt` (srtt):

- `min_rtt` is the *minimum* RTT the kernel has seen on the socket — it approximates the propagation/path floor and is largely free of transient queueing and load. That makes it a clean proxy for distance/path, which is exactly what bands are about.
- `tcp_info_rtt` (srtt) is useful as a *current latency* feature and, together with `tcp_info_rtt_var`, as a **jitter** signal — but it inflates under load, so it's noisier for geography.

**All RTT fields are microseconds** — divide by 1000 for milliseconds. RTT spans several orders of magnitude (0.1 ms intra-DC to 300 ms mobile), so **analyze it on a log scale**; the modes that correspond to bands are far clearer in `log10(min_rtt_ms)` than in linear space.

## Data preparation

This is the make-or-break step. Two issues dominate.

**1. Fix the grain.** The export is one row per socket **per poll** (default every 10s), so a long-lived connection contributes many rows. If you cluster raw rows, long flows dominate and you're really clustering "poll-seconds," not sockets. **Aggregate to one feature vector per socket**, keyed by `inet_diag_msg_socket_cookie` + `hostname` + `netns` (the cookie is unique only within a host/namespace). Sensible aggregations: `MIN(tcp_info_min_rtt)`, median or last srtt, `MAX(tcp_info_delivery_rate)`, and the **last** value of cumulative counters.

**2. Filter and normalize.**

- Keep `inet_diag_msg_state = 1` (ESTABLISHED). Drop listeners (`10`), `TIME_WAIT`, etc.
- Drop loopback / intra-host noise (source == destination, `127.0.0.0/8`, `::1`).
- Require a minimum lifetime — e.g. ≥ N samples or ≥ some bytes — so ephemeral sockets with one noisy RTT sample don't pollute the bands (survivorship filtering).
- Convert units: µs → ms for RTT; bytes/s → Mbit/s for throughput.
- Counters (`tcp_info_bytes_*`, `tcp_info_total_retrans`, `tcp_info_segs_*`) are **cumulative over the socket lifetime** — use the last value per socket, or deltas between consecutive polls for rates.
- Derive the **data-center** dimension. xtcp2 doesn't emit "DC" directly — derive it from your `hostname` convention, or set it explicitly with the daemon's `-label`/`-tag` (carried in the `label`/`tag` columns).

The per-socket feature table (one row per socket) is the input to everything below. See the [worked example](#worked-example) for the SQL.

## Finding RTT bands

This is a one-dimensional clustering problem on `log(min_rtt)`.

**Look before you cluster.** Plot a histogram and a kernel-density estimate (KDE) of `log10(min_rtt_ms)`. You'll usually *see* the modes (one bump per band) and the valleys between them. That sanity-checks everything that follows.

**Method A — Gaussian Mixture Model + BIC (recommended, adaptive).** Fit a 1-D GMM to `log(min_rtt)` and let **BIC** choose the number of components. Each component is a band; the boundaries are where adjacent components cross over. This auto-discovers how many bands exist *today* — re-fit per time window (e.g. daily, per DC) and the band count/locations track drift automatically. This is the method to build the production pipeline on.

**Method B — natural breaks (simple, explainable).** Jenks natural-breaks optimization (or finding the valleys/minima of the KDE) on `log(min_rtt)` gives defensible cut points that are easy to explain to stakeholders ("we split where the data has gaps"). Good for a first pass or when a GMM is overkill.

**Method C — quantile bands in the warehouse (quick win).** `NTILE(n)` or `APPROX_PERCENTILE` in Snowflake gives instant coarse bands with zero data movement. Caveat: quantiles cut the data into *equal-sized* groups, which is **not** the same as finding natural modes — a quantile boundary can land in the middle of a real band. Use it for a fast look, not as the definition of a band.

**Label and validate.** Name each band by its centroid RTT, then **confirm the physical story** with independent columns: `inet_diag_msg_socket_dest_asn`, an IP-geolocation join on the decoded `inet_diag_msg_socket_destination`, or `inet_diag_msg_socket_destination_port`. The lowest band should be mostly intra-DC peers, the next mostly CDN ASNs in your metro, and so on. If the physical story doesn't match the statistical band, investigate before trusting it. Keep the labels **derived from the centroids**, not hardcoded.

**Track over time and per DC.** Persist each run's band centroids and boundaries keyed by (data center, day) as a time series. Now you can compare EU vs US vs AU, watch a band's RTT creep, and alert when a boundary jumps (a routing change, a new POP, or an outage).

## Multi-feature clustering

RTT is the headline, but sockets cluster on more than latency. Build a standardized feature vector per socket and cluster in several dimensions:

- `log(min_rtt_ms)` — path/distance
- `log(throughput_mbps)` — capacity / flow size
- `retrans_rate` — loss (`bytes_retrans / bytes_sent`)
- `rtt_var / min_rtt` — *relative* jitter (dimensionless)
- `log(snd_cwnd)` — window the path sustains

Standardize (z-score) after log-transforming the heavy-tailed features. Algorithm trade-offs:

| Algorithm | Pros | Cons |
|---|---|---|
| **K-means** | Fast, simple baseline | Must pick K; assumes round, equal-size clusters |
| **GMM** | Soft assignments; BIC picks K; elliptical clusters | Assumes Gaussian components |
| **HDBSCAN** | No K; arbitrary shapes; **labels outliers as noise** | Sensitive to `min_cluster_size`; needs scaled features |

**HDBSCAN is the recommended default** here — it doesn't need a predetermined cluster count and its built-in noise label naturally captures the "outliers" band (item 4 above) instead of forcing every socket into a group. Use PCA or UMAP to project to 2-D for a scatter plot colored by cluster. Validate with silhouette score (or BIC for GMM), **stability across time windows** (do the same clusters reappear tomorrow?), and **external agreement** — clusters should line up with `dest_asn`, `congestion_algorithm_string`, or DC.

## Other useful analyses

- **Throughput bands.** `log(tcp_info_delivery_rate)` is heavy-tailed; cluster it to separate "elephant" flows from "mice." Exclude `tcp_info_delivery_rate_app_limited = 1` rows when you want *path* capacity (those flows were limited by the application, not the network).
- **Retransmission / loss bands.** `bytes_retrans / bytes_sent` (or `total_retrans / segs_out`) splits healthy (~0) from lossy paths. Cross-tab with the RTT band — high-RTT mobile paths often also show elevated loss.
- **Congestion-algorithm comparison.** Group by `congestion_algorithm_string` (e.g. BBR vs CUBIC) and compare RTT/throughput/loss distributions for the same destination band.
- **Per-ASN / per-CDN performance.** Aggregate by `inet_diag_msg_socket_dest_asn` to rank CDN edges or transit providers by latency and loss from each DC.
- **Diurnal patterns.** Bucket by hour-of-day (`timestamp_ns`); mobile/last-mile RTT typically rises in the evening peak. Useful for capacity planning.
- **Anomaly / drift detection.** Monitor band centroids over time; a sudden shift is a strong signal of a routing change or incident.

## Worked example

The snippets below are **illustrative — adapt names and thresholds to your environment.**

**Step 1 — per-socket feature table (DuckDB; near-identical in Snowflake).** Aggregate the poll-grain rows to one row per socket.

```sql
WITH socket AS (
  SELECT
    hostname,
    netns,
    inet_diag_msg_socket_cookie                       AS cookie,
    -- DC from a hostname convention like "iad1-web-07"; adapt the regex,
    -- or use the daemon's -label which lands in the `label` column.
    regexp_extract(hostname, '^([a-z]+[0-9]+)', 1)    AS dc,
    inet_diag_msg_socket_dest_asn                     AS dest_asn,
    MIN(tcp_info_min_rtt) / 1000.0                    AS min_rtt_ms,
    MEDIAN(tcp_info_rtt)  / 1000.0                    AS srtt_ms,
    MEDIAN(tcp_info_rtt_var) / 1000.0                 AS rtt_var_ms,
    MAX(tcp_info_delivery_rate) * 8.0 / 1e6           AS mbps,
    MAX(tcp_info_snd_cwnd)                            AS cwnd,
    -- cumulative counters: last value ≈ MAX over the socket's life
    MAX(tcp_info_bytes_sent)                          AS bytes_sent,
    MAX(tcp_info_bytes_retrans)                       AS bytes_retrans,
    ANY_VALUE(congestion_algorithm_string)            AS congestion,
    COUNT(*)                                          AS samples
  FROM read_parquet('s3://bucket/xtcp/**/*.parquet', hive_partitioning => true)
  WHERE inet_diag_msg_state = 1                       -- ESTABLISHED only
  GROUP BY 1,2,3,4,5
)
SELECT
  *,
  CASE WHEN bytes_sent > 0 THEN bytes_retrans::DOUBLE / bytes_sent ELSE 0 END AS retrans_rate
FROM socket
WHERE samples >= 3            -- survivorship: drop ephemeral sockets
  AND min_rtt_ms > 0;
```

**Step 2 — RTT bands in Python (GMM + BIC).**

```python
import numpy as np, pandas as pd
from sklearn.mixture import GaussianMixture

feat = pd.read_parquet("socket_features.parquet")      # the table from step 1
x = np.log10(feat["min_rtt_ms"].to_numpy()).reshape(-1, 1)

# Let BIC choose the number of bands (1..8).
models = {k: GaussianMixture(k, covariance_type="full", random_state=0).fit(x)
          for k in range(1, 9)}
k = min(models, key=lambda k: models[k].bic(x))
gmm = models[k]
feat["band"] = gmm.predict(x)

# Order bands by RTT and summarize.
centroids = (10 ** gmm.means_.ravel())
order = np.argsort(centroids)
print(f"{k} bands; centroids (ms):", np.round(centroids[order], 2))
print(feat.groupby("band")["min_rtt_ms"].describe()[["count", "mean", "min", "max"]])
# Plot log10(min_rtt_ms) histogram + the fitted components to eyeball the fit.
```

**Step 3 — multi-feature clusters (HDBSCAN).**

```python
import hdbscan
from sklearn.preprocessing import StandardScaler

cols = ["min_rtt_ms", "mbps", "rtt_var_ms", "cwnd"]
F = feat[cols].copy()
F[["min_rtt_ms", "mbps", "cwnd"]] = np.log10(F[["min_rtt_ms", "mbps", "cwnd"]].clip(lower=1e-6))
F["rel_jitter"] = feat["rtt_var_ms"] / feat["min_rtt_ms"]
Z = StandardScaler().fit_transform(F)

labels = hdbscan.HDBSCAN(min_cluster_size=50).fit_predict(Z)  # -1 == outlier/noise
feat["cluster"] = labels
print(feat.groupby("cluster")[["min_rtt_ms", "mbps", "retrans_rate"]].median())
```

**Quick warehouse bands (Snowflake).** When you just need coarse buckets fast:

```sql
SELECT APPROX_PERCENTILE(tcp_info_min_rtt/1000.0, 0.5)  AS p50_ms,
       APPROX_PERCENTILE(tcp_info_min_rtt/1000.0, 0.9)  AS p90_ms,
       NTILE(5) OVER (ORDER BY tcp_info_min_rtt)        AS rtt_quintile
FROM xtcp_flat_records
WHERE inet_diag_msg_state = 1;
```

Snowflake users who want clustering in-warehouse can also use Snowflake ML / Cortex's k-means on the per-socket feature table instead of exporting to Python.

## Pitfalls

- **RTT is microseconds** — divide by 1000 for ms. Easy to be off by 1000×.
- **`min_rtt` vs srtt** — band on `min_rtt`; srtt is inflated by load and queueing.
- **Cluster per-socket, not per-poll-row** — otherwise long-lived flows dominate the model.
- **Counters are cumulative** — use the last value per socket, or deltas for rates; don't sum across polls.
- **Survivorship** — filter out very short / low-byte sockets; their RTT is a single noisy sample.
- **App-limited throughput** — `tcp_info_delivery_rate_app_limited = 1` means the app, not the network, capped the rate; exclude for path-capacity work.
- **Addresses are raw bytes** — decode with `inet_diag_msg_family` before any geo/ASN join (see the [parquet decoding cheat sheet](parquet-format.md#decoding-cheat-sheet)).
- **Bands drift** — re-fit on a schedule; a model trained last quarter will mislabel today.
- **Per-host clocks** — `timestamp_ns` is each host's wall clock; don't assume sub-second alignment across machines.

## See also

- [Parquet format](parquet-format.md) — how to load the data and what every column means.
- [Protobuf formats](protobuf-formats.md) — the authoritative schema and field semantics.
- [Output formats & destinations](output-and-destinations.md) — the export pipeline and the humanized formats.
