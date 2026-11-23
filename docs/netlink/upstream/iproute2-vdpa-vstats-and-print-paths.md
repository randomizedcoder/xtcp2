# iproute2 `vdpa` bugs: vstats mislabels its queues, plus a u16/u32 width mismatch, trailing-space JSON keys, and some latent print gaps

**Status (2026-11-14):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in this directory. No patch, no PR, no sign-off.
- **Source:** the local `~/Downloads/iproute2` checkout, `a3c19931` (`v7.3.0-27-ga3c19931`), and the Linux checkout `46fec1c3029b` (`drivers/vdpa/vdpa.c`). The binary the vdpa corpus was captured against is nixpkgs' `iproute2-7.2.0`; both carry the `vdpa/vdpa.c` code below unchanged.
- **How they were found:** porting vdpa's read-only show paths to `internal/govdpa` (the genl-clones arc, 2R/V084) and auditing `vdpa/vdpa.c`'s print paths against `pkg/xtcpnl`'s `ParseVdpa` (2P/V083), with the captured `vdpa_sim_net` corpus as the oracle.
- **Reachability:** findings 1–4 are reached by the committed `vdpa_sim_net` corpus (`pkg/xtcpnl/testdata/7_3_1/ws/vdpa/`) and are output-correctness or usability faults, not memory-safety ones. Findings 5–7 are latent on that corpus (they need a larger device, or a specific failure). Finding 8 is a robustness note, root-only, with no privilege boundary crossed; it is not demonstrated here.

## Contents

- [Summary](#summary)
- [1. `print_queue_type` mislabels rx and control queues when the kernel omits `MAX_VQP`](#1-print_queue_type-mislabels-rx-and-control-queues-when-the-kernel-omits-max_vqp)
- [2. `print_queue_type` reads the u32 `QUEUE_INDEX` with `mnl_attr_get_u16`](#2-print_queue_type-reads-the-u32-queue_index-with-mnl_attr_get_u16)
- [3. `pr_out_dev_net_config` emits JSON keys `"link "` and `"link_announce "` with trailing spaces](#3-pr_out_dev_net_config-emits-json-keys-link--and-link_announce--with-trailing-spaces)
- [4. `qidx` is parsed as optional but the kernel requires it](#4-qidx-is-parsed-as-optional-but-the-kernel-requires-it)
- [5. `max_supported_vqs` is a u32 printed with `%d`](#5-max_supported_vqs-is-a-u32-printed-with-d)
- [6. `min_vq_size` is sent by the kernel but neither in the policy nor printed](#6-min_vq_size-is-sent-by-the-kernel-but-neither-in-the-policy-nor-printed)
- [7. A failing `-j` vstats still prints `{"vstats":{}}`](#7-a-failing--j-vstats-still-prints-vstats)
- [8. Handle buffers are filled with unbounded `sprintf`](#8-handle-buffers-are-filled-with-unbounded-sprintf)
- [How xtcp2 handles them](#how-xtcp2-handles-them)

## Summary

| # | Function | File | Fault | Severity |
|---|---|---|---|---|
| 1 | `print_queue_type` | `vdpa/vdpa.c:894` | `queue_type` is derived from `max_vqp`, which defaults to 0 because the kernel omits `MAX_VQP` from the vstats reply unless MQ/RSS is negotiated; so queue 0 prints `control_vq` and queue 2 prints `rx` — both wrong | wrong label on every single-queue-pair device |
| 2 | `print_queue_type` | `vdpa/vdpa.c:899` | `QUEUE_INDEX` is `NLA_U32` on the wire but read with `mnl_attr_get_u16` | wrong index ≥ 2¹⁶ (latent on little-endian) |
| 3 | `pr_out_dev_net_config` | `vdpa/vdpa.c:797,799` | the JSON object keys are `"link "` and `"link_announce "`, trailing space included | malformed/ugly JSON keys |
| 4 | `vdpa_argv_parse` / `cmd_dev_vstats_show` | `vdpa/vdpa.c:343,993` | `qidx` is passed as `o_optional`, so iproute2 accepts its absence, but the kernel's stats `doit` requires it and returns `-EINVAL` | confusing error, help says nothing |
| 5 | `pr_out_mgmtdev` | `vdpa/vdpa.c:575` | `max_supported_vqs` is a u32 (`mnl_attr_get_u32`) printed with `%d` | negative number at ≥ 2³¹ |
| 6 | `pr_out_dev` / `vdpa_policy` | `vdpa/vdpa.c:83,664` | the kernel sends `VDPA_ATTR_DEV_MIN_VQ_SIZE`, but it is in neither `vdpa_policy` nor `pr_out_dev` | a kernel-reported field is silently dropped |
| 7 | `cmd_dev_vstats_show` | `vdpa/vdpa.c:997` | the JSON section is opened before `sndrcv` and closed after, unconditionally, so a kernel error leaves `{"vstats":{}}` on stdout with rc 1 | empty result object on failure |
| 8 | `pr_out_dev` / handle starts | `vdpa/vdpa.c:675` etc. | kernel-returned names are copied into fixed stack buffers with unbounded `sprintf` | robustness, root-only, no boundary crossed |

## 1. `print_queue_type` mislabels rx and control queues when the kernel omits `MAX_VQP`

`vdpa dev vstats show <dev> qidx <n>` prints a `queue_type` for the queue.
iproute2 derives the type from the queue index and the max-virtqueue-pairs
count:

```c
/* vdpa/vdpa.c:894 */
static void print_queue_type(struct nlattr *attr, uint16_t max_vqp, uint64_t features)
{
	bool is_ctrl = false;
	uint16_t qidx = 0;

	qidx = mnl_attr_get_u16(attr);
	is_ctrl = features & BIT(VIRTIO_NET_F_CTRL_VQ) && qidx == 2 * max_vqp;
	if (!is_ctrl) {
		if (qidx & 1)
			print_string(..., "tx");
		else
			print_string(..., "rx");
	} else {
		print_string(..., "control_vq");
	}
}
```

`max_vqp` comes from `VDPA_ATTR_DEV_NET_CFG_MAX_VQP` in the same reply
(`pr_out_dev_net_vstats`, `vdpa/vdpa.c:924`), and it stays **0** when that
attribute is absent. The kernel omits it unless multiqueue or RSS is
negotiated:

```c
/* drivers/vdpa/vdpa.c:837 (Linux 46fec1c3029b) */
static int vdpa_dev_net_mq_config_fill(struct sk_buff *msg, u64 features,
				       const struct virtio_net_config *config)
{
	u16 val_u16;

	if ((features & BIT_ULL(VIRTIO_NET_F_MQ)) == 0 &&
	    (features & BIT_ULL(VIRTIO_NET_F_RSS)) == 0)
		return 0;        /* <-- no MAX_VQP attribute emitted */
	...
	return nla_put_u16(msg, VDPA_ATTR_DEV_NET_CFG_MAX_VQP, val_u16);
}
```

A plain `vdpa_sim_net` device negotiates `CTRL_VQ` but not `MQ`/`RSS`, so
`MAX_VQP` is absent and `max_vqp == 0`. Then `is_ctrl` reduces to
`features & CTRL_VQ && qidx == 0`, i.e. the **rx** queue (index 0) is
labelled `control_vq`, while the real control queue (index `2 * 1 == 2`)
hits the `else` and, being even, is labelled `rx`.

This is not hypothetical — it is exactly what the captured corpus shows.
A 3-virtqueue `vdpa_sim_net` device (rx=0, tx=1, cvq=2), `vdpa -j dev
vstats show vdpa0 qidx N`:

```
qidx 0 → {"queue_type":"control_vq","rx packets":0,...}   # rx stats, labelled control_vq
qidx 1 → {"queue_type":"tx","tx packets":0,...}            # correct
qidx 2 → {"queue_type":"rx","cvq requests":0,...}          # control-vq stats, labelled rx
```

The vendor stat *names* in each reply (`rx packets`, `tx packets`,
`cvq requests`) come straight from the driver and are right; only the
`queue_type` label iproute2 computes is wrong. Labels 0 and 2 are swapped
with each other and with reality. The root cause is deriving the type from
a count the kernel did not send for this device class; the label is only
correct when `MAX_VQP` is present (a multiqueue device), which the common
single-pair case is not.

## 2. `print_queue_type` reads the u32 `QUEUE_INDEX` with `mnl_attr_get_u16`

In the same function, the queue index is read as a u16:

```c
/* vdpa/vdpa.c:899 */
	qidx = mnl_attr_get_u16(attr);
```

But `VDPA_ATTR_DEV_QUEUE_INDEX` is a u32 on the wire. The kernel puts it
with `nla_put_u32` and validates the request copy as `NLA_U32`:

```c
/* drivers/vdpa/vdpa.c */
1209:	if (nla_put_u32(msg, VDPA_ATTR_DEV_QUEUE_INDEX, index))
1462:	index = nla_get_u32(info->attrs[VDPA_ATTR_DEV_QUEUE_INDEX]);
1503:	[VDPA_ATTR_DEV_QUEUE_INDEX] = { .type = NLA_U32 },
```

`mnl_attr_get_u16` reads the first 2 bytes of the 4-byte payload. On
little-endian hosts that is the low half, so indices below 65536 happen to
print correctly and the corpus does not expose it; a queue index of 2¹⁶ or
more would print its low 16 bits. iproute2's own request builder sends the
index with the matching width (`mnl_attr_put_u32`, `vdpa/vdpa.c:261`), so
the read side is simply inconsistent with both the kernel and its own
write side. `mnl_attr_get_u32` is the fix.

## 3. `pr_out_dev_net_config` emits JSON keys `"link "` and `"link_announce "` with trailing spaces

`vdpa dev config show` prints the link state. The JSON key argument carries
a trailing space:

```c
/* vdpa/vdpa.c:797 */
	print_string(PRINT_ANY, "link ", "link %s ",
		     ...);
	print_bool(PRINT_ANY, "link_announce ", "link_announce %s ",
		     ...);
```

The first argument to `print_string`/`print_bool` is the JSON object key;
the second is the text-mode format. The key should be `"link"`, but it is
`"link "`. The result is visible in the captured `-j` output:

```json
{"config":{"vdpa0":{"mac":"00:11:22:33:44:55","link ":"up","link_announce ":false,"mtu":1500,...}}}
```

So a JSON consumer must look up `"link "` (with the space), not `"link"`.
Every sibling key in the same object (`mac`, `mtu`, `negotiated_features`)
is clean; only these two carry the stray space, which reads like the text
format string `"link %s "` leaking into the key argument. Dropping the
space from the key argument fixes it without touching the text output.

## 4. `qidx` is parsed as optional but the kernel requires it

`cmd_dev_vstats_show` passes the queue index as an **optional** arg:

```c
/* vdpa/vdpa.c:993 */
	err = vdpa_argv_parse_put(nlh, vdpa, argc, argv,
				  VDPA_OPT_VDEV_HANDLE, VDPA_OPT_QUEUE_INDEX);
```

The last argument is `o_optional`, so `vdpa_argv_parse` only consumes a
`qidx` if one is typed (`vdpa/vdpa.c:343`) and never enforces its presence.
The `argc != 1 && argc != 3` guard above it (`vdpa/vdpa.c:987`) permits
`argc == 1` — the device handle alone, no `qidx`. But the kernel's stats
`doit` rejects a request with no index:

```c
/* drivers/vdpa/vdpa.c:1454 */
	if (!info->attrs[VDPA_ATTR_DEV_QUEUE_INDEX])
		return -EINVAL;
```

So `vdpa dev vstats show vdpa0` is accepted by iproute2, sent, and comes
back `-EINVAL` from the kernel with no clue as to why — the captured
corpus has exactly this negative (rc 1, "Invalid argument"). The index is
effectively mandatory; treating it as `o_required`, or at least rejecting
`argc == 1`, would turn the kernel's opaque EINVAL into a local usage
message.

## 5. `max_supported_vqs` is a u32 printed with `%d`

`pr_out_mgmtdev` reads the management device's max VQ count as a u32 and
prints it signed:

```c
/* vdpa/vdpa.c:570 */
	if (tb[VDPA_ATTR_DEV_MGMTDEV_MAX_VQS]) {
		uint32_t num_vqs;

		num_vqs = mnl_attr_get_u32(tb[VDPA_ATTR_DEV_MGMTDEV_MAX_VQS]);
		print_uint(PRINT_ANY, "max_supported_vqs", "  max_supported_vqs %d", num_vqs);
	}
```

The value is a `uint32_t` (and `NLA_U32` on the wire, policy at
`vdpa/vdpa.c:92`), but the text format is `%d`. A management device
reporting 2³¹ or more supported VQs would print a negative number in text
mode. `vdpa_sim_net` reports a small count, so the corpus never triggers
it; `%u` is the fix. (Latent, cosmetic — a device with that many VQs is
not realistic today.)

## 6. `min_vq_size` is sent by the kernel but neither in the policy nor printed

`dev show` prints `max_vq_size` but never `min_vq_size`, although the
kernel sends both in the `DEV_GET` reply:

```c
/* drivers/vdpa/vdpa.c:736 */
	if (nla_put_u16(msg, VDPA_ATTR_DEV_MAX_VQ_SIZE, max_vq_size))
		goto msg_err;
	if (nla_put_u16(msg, VDPA_ATTR_DEV_MIN_VQ_SIZE, min_vq_size))
		goto msg_err;
```

iproute2's `pr_out_dev` handles only the max (`vdpa/vdpa.c:688`), and
`VDPA_ATTR_DEV_MIN_VQ_SIZE` is absent from `vdpa_policy` entirely
(`vdpa/vdpa.c:83`–`94` lists `MAX_VQ_SIZE` but no `MIN_VQ_SIZE`). So the
attribute is dropped at parse and never printed: a field the kernel
reports is invisible in both text and JSON. Adding it to the policy and a
`print_uint` line beside `max_vq_size` would surface it. (Information
loss, not a crash.)

## 7. A failing `-j` vstats still prints `{"vstats":{}}`

`cmd_dev_vstats_show` opens the JSON section before the round-trip and
closes it after, unconditionally:

```c
/* vdpa/vdpa.c:997 */
	pr_out_section_start(vdpa, "vstats");
	err = mnlu_gen_socket_sndrcv(&vdpa->nlg, nlh, cmd_dev_vstats_show_cb, vdpa);
	pr_out_section_end(vdpa);
	return err;
```

When the kernel returns an error (e.g. the EINVAL of finding 4, or an out-
of-range `qidx`), `cmd_dev_vstats_show_cb` never runs, so nothing is added
to the section, but the section is still opened and closed. In `-j` mode
that leaves `{"vstats":{}}` on stdout even though the command failed with
rc 1 — an empty result object rather than no result. The text-mode case is
the same but invisible (the section is blank). Starting the section only
after a successful round-trip, or on the first callback, would avoid
emitting a result shell for a failed command.

## 8. Handle buffers are filled with unbounded `sprintf`

`pr_out_dev` composes the management-device handle into a fixed stack
buffer with `sprintf` and no bound:

```c
/* vdpa/vdpa.c:669 */
	char mgmtdev_buf[128];
	...
	sprintf(mgmtdev_buf, "%s/%s", mdev_bus_name, mdev_name);   /* vdpa/vdpa.c:675 */
```

The handle starts (`__pr_out_vdev_handle_start`, and the `SPRINT_BUF`
handle buffers elsewhere) do the same with smaller buffers. The names are
kernel-returned device/bus names. This is a robustness note, not a reported
exploit: creating a vdpa device with an over-long name needs
`GENL_ADMIN_PERM` (root), which is the same privilege as running the tool,
so no privilege boundary is crossed. It was **not** demonstrated live (the
genl-clones arc is read-only and recorded-only). Using `snprintf` with the
buffer size would make it robust regardless. Noted for completeness; lowest
priority of the set.

## How xtcp2 handles them

- `pkg/xtcpnl`'s `ParseVdpa` reads `VDPA_ATTR_DEV_QUEUE_INDEX` as a u32
  (its real wire width, finding 2) and keeps every vstats attribute in
  wire order, so a consumer that is not emulating vdpa gets the right
  index and can pair each vendor stat with the queue the kernel actually
  reported. It also decodes `MIN_VQ_SIZE` when present (finding 6), so the
  field is not lost on the decode path.
- `internal/govdpa` reproduces vdpa's *captured* output byte for byte —
  the mislabelled `queue_type` (finding 1), the u16-narrowed read
  (finding 2), the `"link "`/`"link_announce "` keys (finding 3) and the
  `{"vstats":{}}` empty section (finding 7) all included — because that is
  what parity means. The faults live in vdpa's own print and argument
  paths; `ParseVdpa` is fuzzed (`FuzzParseVdpa`) and bounded by attribute
  length throughout, so none of them is a decode hazard for xtcp2.
