# iproute2 `devlink` bugs: a u64 resource field truncated to 32 bits, and a param switch missing its u64 arm

**Status (2026-11-13):**

- **Not submitted to netdev.** These are drafts for the user's review, recorded here only, like the other write-ups in this directory. No patch, no PR, no sign-off.
- **Source:** the local `~/Downloads/iproute2` checkout, `a3c19931` (`v7.3.0-27-ga3c19931`). The binary the devlink corpus was captured against is nixpkgs' `iproute2-7.2.0`; both carry the code below unchanged.
- **How they were found:** porting devlink's read-only show paths to `internal/godevlink` (the genl-clones arc, 1R/V081) and auditing `devlink/devlink.c`'s print paths against `pkg/xtcpnl`'s `ParseDevlink` (1P/V080).
- **Reachability:** neither is reachable from the `netdevsim` corpus. #1 needs a driver whose resource reports an occupancy, minimum or granularity of 2³² or more; netdevsim's are all small. #2 is a latent inconsistency the current kernel never reaches, because no devlink parameter has a u64 type. Both are output-correctness faults, not memory-safety ones.

## Contents

- [Summary](#summary)
- [1. `resource_show` prints three u64 fields through `print_uint`](#1-resource_show-prints-three-u64-fields-through-print_uint)
- [2. `pr_out_param_value_print` has no `DEVLINK_VAR_ATTR_TYPE_U64` case](#2-pr_out_param_value_print-has-no-devlink_var_attr_type_u64-case)
- [How xtcp2 handles them](#how-xtcp2-handles-them)

## Summary

| # | Function | File | Fault | Severity |
|---|---|---|---|---|
| 1 | `resource_show` | `devlink/devlink.c:9089,9094,9097` | `occ`, `size_min` and `size_gran` are `uint64_t` but are passed to `print_uint`, whose value parameter is `unsigned int`: any value ≥ 2³² is truncated to its low 32 bits | wrong number on large resources |
| 2 | `pr_out_param_value_print` | `devlink/devlink.c:3650` | the value-type `switch` handles U8/U16/U32/STRING/FLAG/U64_ARRAY but not plain `DEVLINK_VAR_ATTR_TYPE_U64`, so a u64 param value prints its label with no number | latent (no u64 param exists yet) |

## 1. `resource_show` prints three u64 fields through `print_uint`

A devlink resource's sizes are all `uint64_t` (`struct resource`,
`devlink/devlink.c:7579`), and the driver fills them with
`mnl_attr_get_u64` (`devlink/devlink.c:8958`):

```c
/* devlink/devlink.c:7583 */
	uint64_t size_min;
	uint64_t size_max;
	uint64_t size_gran;
	...
	uint64_t size_occ;
```

`resource_show` prints `size` and `size_max` correctly, with `pr_out_u64`,
but prints `occ` (`size_occ`), `size_min` and `size_gran` with `print_uint`:

```c
/* devlink/devlink.c:9085 */
	pr_out_u64(dl, "size", resource->size);
	if (resource->size != resource->size_new)
		pr_out_u64(dl, "size_new", resource->size_new);
	if (resource->occ_valid)
		print_uint(PRINT_ANY, "occ", " occ %u",  resource->size_occ);
	...
	if (resource->size_min != resource->size_max) {
		print_uint(PRINT_ANY, "size_min", " size_min %u",
			   resource->size_min);
		pr_out_u64(dl, "size_max", resource->size_max);
		print_uint(PRINT_ANY, "size_gran", " size_gran %u",
			   resource->size_gran);
	}
```

`print_uint`'s value parameter is an `unsigned int`, not a 64-bit type
(`lib/json_print.c:140`, `_PRINT_FUNC(uint, unsigned int)`), so the
`uint64_t` argument is converted to 32 bits at the call before either the
text (`"%u"`) or the JSON path sees it. A resource whose occupancy,
minimum or granularity is 2³² or more prints the wrong (low-32-bit) number;
`size` and `size_max` beside it stay correct, which makes the discrepancy
easy to miss. `pr_out_u64` (the `__u64`-wide printer the sibling fields use)
is the fix.

netdevsim's resources are small (`occ` 3, `size_min` 0, `size_gran` 1), so
the captured corpus never triggers it; a driver with a multi-gigabyte
resource would.

## 2. `pr_out_param_value_print` has no `DEVLINK_VAR_ATTR_TYPE_U64` case

`pr_out_param_value_print` prints a parameter's value or default. It emits
the label first, unconditionally, then switches on the value type:

```c
/* devlink/devlink.c:3650 */
	print_string(PRINT_FP, NULL, " %s ", label);

	switch (nla_type) {
	case DEVLINK_VAR_ATTR_TYPE_U8:   ...
	case DEVLINK_VAR_ATTR_TYPE_U16:  ...
	case DEVLINK_VAR_ATTR_TYPE_U32:  ...
	case DEVLINK_VAR_ATTR_TYPE_STRING: ...
	case DEVLINK_VAR_ATTR_TYPE_FLAG:   ...
	case DEVLINK_VAR_ATTR_TYPE_U64_ARRAY: ...
	}
```

There is no `DEVLINK_VAR_ATTR_TYPE_U64` arm. If `nla_type` were u64, the
text output would be the label alone — `" value "` or `" default "`, a
trailing space with no number — and the JSON output would omit the value
key entirely (the leading `print_string` is `PRINT_FP`, text only). The
sibling var-attr switches in the same file do handle u64
(`devlink/devlink.c:3920`, `4058`), so this one is simply inconsistent.

It is latent: `nla_type` here comes from `DEVLINK_ATTR_PARAM_TYPE`
(`pr_out_param`), and the kernel's `devlink_param_type` has no u64 member
(only U8/U16/U32/STRING/BOOL), so no parameter is ever typed u64 today.
A future u64 parameter type, or any reuse of this function for a u64 value,
would reach the gap.

## How xtcp2 handles them

- `pkg/xtcpnl`'s `ParseDevlink` reads every resource size as a `*uint64`
  (`DevlinkResource.Occ`, `SizeMin`, `SizeGran`) and every param value as a
  `*uint64` or typed payload (`decodeParamValue`), so no field is narrowed
  on the way in. `internal/godevlink` reproduces devlink's *captured* output
  byte for byte, truncation and all, because that is what parity means; the
  decoder keeps the full width so a consumer that is not emulating devlink
  gets the real number.
- Neither fault is a decode hazard for xtcp2: both are in devlink's own
  print path, and `ParseDevlink` is fuzzed (`FuzzParseDevlink`) and bounded
  by attribute length throughout.
