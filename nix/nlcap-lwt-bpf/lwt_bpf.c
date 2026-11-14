// SPDX-License-Identifier: GPL-2.0
/*
 * lwt_bpf.c: the workbench's BPF lwt object (NC8-T02 of
 * docs/netlink/goip-parity-next.md). One program per lwt hook, each a
 * pass-through (pass_in, pass_out, pass_xmit: a symbol may not share its
 * section's name), so `ip route add ... encap bpf in|out|xmit obj
 * /run/nlcap-ws/lwt_bpf.o sec lwt_in|lwt_out|lwt_xmit` installs and the
 * capture records LWT_BPF_IN/OUT/XMIT. ip names each program
 * "lwt_bpf.o:[<sec>]". GPL, as BPF_PROG_TYPE_LWT_* helpers may require.
 */
#include <linux/bpf.h>

#define SEC(name) __attribute__((section(name), used))

SEC("lwt_in")
int pass_in(struct __sk_buff *skb)
{
	return BPF_OK;
}

SEC("lwt_out")
int pass_out(struct __sk_buff *skb)
{
	return BPF_OK;
}

SEC("lwt_xmit")
int pass_xmit(struct __sk_buff *skb)
{
	return BPF_OK;
}

char _license[] SEC("license") = "GPL";
