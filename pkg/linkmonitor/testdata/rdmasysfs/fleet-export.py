#!/usr/bin/env python3
"""Export anonymized one-port fixtures from a fleet RDMA rescan.

    python3 -I fleet-export.py FLEET_REPO OUT_DIR GENERATED_GO

FLEET_REPO is a checkout holding snapshot-rdma/hosts/<id>/rdma-sysfs.txt
(two samples, t0 and t1) and snapshot/hosts/<id>/uname.txt. Ports are grouped
by NIC model, link layer and the exact set of counter files, unreadable files
and saturated registers; each group becomes one fixture from one
representative port, renamed to port 1.

Nothing that identifies a host is written: no host id, hostname, device or
netdev name, PCI address, GUID, LID or data center. Fixtures keep only the
attributes capture.sh records, the counter values, the kernel release, and
the firmware versions seen in the group.

The generated Go table pins, per fixture, the rate parsed here (independently
of rdmaRate), the files the catalog drops, the unreadable files, and the
bytes-per-packet bounds for ports that moved traffic between samples.
"""

import collections
import os
import re
import subprocess
import sys

COUNTER_GROUPS = ("counters", "counters_ext", "hw_counters")
DEVICE_FILES = ("board_id", "fw_ver", "hca_type", "node_type")
PORT_FILES = ("link_layer", "phys_state", "rate", "state")
# Fixed-width registers that stop at their maximum instead of wrapping.
SATURATED = {str(2**16 - 1), str(2**32 - 1), str(2**64 - 1)}
READ_ERROR = "READ_ERROR"
# A packet is at least a minimum Ethernet frame on RoCE, and at most a 4 KiB
# MTU payload plus IB headers (LRH, GRH, BTH, RETH, ICRC, VCRC) on InfiniBand.
ETHERNET_MIN_BYTES_PER_PACKET = 64
INFINIBAND_MAX_BYTES_PER_PACKET = 4096 + 128
# A bulk InfiniBand trace averages close to the 4 KiB MTU; one at or above
# this pins the four-octet scale from below as well.
BULK_BYTES_PER_PACKET = 3500
# Enough packets that the average is a bulk trace rather than a few probes.
MIN_TRAFFIC_PACKETS = 10000

MODELS = {
    "14e4:16d7": "Broadcom BCM57414",
    "14e4:16d8": "Broadcom BCM57416",
    "14e4:1751": "Broadcom BCM57504",
    "8086:1592": "Intel E810-C",
    "8086:1593": "Intel E810-C",
    "8086:159b": "Intel E810-XXV",
    "8086:37d2": "Intel X722",
    "15b3:1013": "ConnectX-4",
    "15b3:1015": "ConnectX-4 Lx",
    "15b3:1017": "ConnectX-5",
    "15b3:1019": "ConnectX-5 Ex",
    "15b3:101b": "ConnectX-6",
    "15b3:101c": "ConnectX-6 VF",
    "15b3:101d": "ConnectX-6 Dx",
    "15b3:101e": "ConnectX-6 Dx VF",
    "15b3:101f": "ConnectX-6 Lx",
    "15b3:1021": "ConnectX-7",
    "15b3:1023": "ConnectX-8",
    "15b3:a2d6": "BlueField-2",
    "15b3:a2dc": "BlueField-3",
}


def parse_sysfs(path):
    """Return [(sample, epoch, {device: (driver, pci, {path: value})})]."""
    samples, current, device = [], None, None
    with open(path, encoding="utf-8", errors="replace") as f:
        for line in f:
            line = line.rstrip("\n")
            m = re.match(r".*# sample (t[01]) ([0-9.]+)$", line)
            if m:
                current = {}
                samples.append((m.group(1), float(m.group(2)), current))
                device = None
                continue
            if current is None:
                continue
            m = re.match(r"=== (\S+) driver=(\S*) pci=(\S*) ===$", line)
            if m:
                device = m.group(1)
                current[device] = (m.group(2), m.group(3).replace("0x", ""), {})
                continue
            if device and "=" in line and not line.startswith(("#", "\x1b", "[")):
                key, value = line.split("=", 1)
                current[device][2][key] = value
    return samples


def kernel_release(path):
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                fields = line.split()
                if len(fields) > 2 and fields[0].endswith("Linux"):
                    return fields[2]
    except FileNotFoundError:
        pass
    return "unknown"


def load_catalog(path):
    with open(path, encoding="utf-8") as f:
        text = f.read()
    return dict(re.findall(r'"((?:counters|counters_ext|hw_counters)/[A-Za-z0-9_]+)":\s*\{"[a-z0-9_]+", ([0-9]+)\}', text))


def rate_bytes(text):
    """Parse '<n>[.<d>] Gb/sec (...)' into bytes per second."""
    m = re.match(r"([0-9]+)(?:\.([0-9]))? Gb/sec", text)
    if not m:
        raise ValueError(f"rate {text!r}")
    tenths = int(m.group(1)) * 10 + int(m.group(2) or 0)
    return tenths * 100_000_000 // 8


def layer_short(layer):
    return {"InfiniBand": "ib", "Ethernet": "eth"}.get(layer, layer.lower())


def ports_of(values):
    return sorted({k.split("/")[1] for k in values if k.startswith("ports/")}, key=int)


def port_view(values, port):
    """Return the port's capture.sh attributes, renamed to port 1."""
    prefix = f"ports/{port}/"
    out = {name: values[name] for name in DEVICE_FILES if name in values}
    for name in PORT_FILES:
        out["ports/1/" + name] = values[prefix + name]
    counters = sorted(k[len(prefix):] for k in values if k.startswith(prefix) and k[len(prefix):].split("/")[0] in COUNTER_GROUPS)
    for name in counters:
        out["ports/1/" + name] = values[prefix + name]
    return out, counters


def delta(view0, view1, name):
    a, b = view0.get("ports/1/" + name, ""), view1.get("ports/1/" + name, "")
    if not (a.isdigit() and b.isdigit()) or a in SATURATED or b in SATURATED:
        return None
    return int(b) - int(a)


def main():
    if len(sys.argv) != 4:
        sys.exit(__doc__)
    fleet, out_dir, generated = sys.argv[1:]
    catalog = load_catalog(os.path.join(os.path.dirname(generated), "rdma_counter_catalog.go"))
    hosts_dir = os.path.join(fleet, "snapshot-rdma", "hosts")
    groups = collections.defaultdict(list)
    for host in sorted(os.listdir(hosts_dir)):
        path = os.path.join(hosts_dir, host, "rdma-sysfs.txt")
        if not os.path.exists(path):
            continue
        samples = parse_sysfs(path)
        if not samples or samples[0][0] != "t0":
            continue
        t0, t1 = samples[0], samples[1] if len(samples) > 1 and samples[1][0] == "t1" else None
        kernel = kernel_release(os.path.join(fleet, "snapshot", "hosts", host, "uname.txt"))
        for device in sorted(t0[2]):
            driver, pci, values = t0[2][device]
            for port in ports_of(values):
                view, counters = port_view(values, port)
                if not counters:
                    continue
                view1 = None
                if t1 and device in t1[2]:
                    later = t1[2][device][2]
                    if f"ports/{port}/rate" in later:
                        view1, _ = port_view(later, port)
                unreadable = tuple(n for n in counters if view["ports/1/" + n] == READ_ERROR)
                saturated = tuple(n for n in counters if view["ports/1/" + n] in SATURATED)
                key = (pci, view["ports/1/link_layer"], tuple(counters), unreadable, saturated)
                elapsed = t1[1] - t0[1] if view1 else 0
                groups[key].append(dict(host=host, device=device, port=port, driver=driver, kernel=kernel, view=view, view1=view1, elapsed=elapsed, fw=values.get("fw_ver", "")))

    os.makedirs(out_dir, exist_ok=True)
    for name in os.listdir(out_dir):
        if name.endswith(".txt"):
            os.remove(os.path.join(out_dir, name))
    rows, counts = [], collections.Counter()
    for key in sorted(groups, key=lambda k: (k[0], k[1], -len(groups[k]), k[2], k[3], k[4])):
        pci, layer, counters, unreadable, saturated = key
        members = groups[key]

        def traffic(m):
            d = delta(m["view"], m["view1"], "counters/port_xmit_packets") if m["view1"] else None
            return d or 0

        def rank(m):
            active = m["view"]["ports/1/state"].endswith("ACTIVE")
            return (-active, -traffic(m), m["host"], m["device"], int(m["port"]))

        rep = min(members, key=rank)
        driver = rep["driver"]
        counts[(driver, pci, layer)] += 1
        name = f"{driver}-{pci.replace(':', '-')}-{layer_short(layer)}-{counts[(driver, pci, layer)]}.txt"
        moved = {}
        if rep["view1"]:
            for n in counters:
                if rep["view1"].get("ports/1/" + n) != rep["view"]["ports/1/" + n]:
                    moved[n] = rep["view1"]["ports/1/" + n]
        model = MODELS.get(pci, pci)
        with open(os.path.join(out_dir, name), "w", encoding="utf-8") as f:
            f.write(f"# source: fleet rescan, one {model} {layer} port of {len(members)} with these counter files\n")
            f.write(f"# model: {model}\n# driver: {driver}\n# pci: {pci}\n# kernel: {rep['kernel']}\n")
            f.write(f"# ports: {len(members)}\n# firmware-seen: {' '.join(sorted({m['fw'] for m in members}))}\n")
            if moved:
                f.write(f"# elapsed: {rep['elapsed']:.3f}\n")
            for path, value in rep["view"].items():
                f.write(f"{path}={value}\n")
            for n, value in moved.items():
                f.write(f"t1:ports/1/{n}={value}\n")

        row = dict(file=name, model=model, layer=layer, ports=len(members), rate=rate_bytes(rep["view"]["ports/1/rate"]),
                   uncataloged=[n for n in counters if n not in catalog], unreadable=list(unreadable), saturated=list(saturated),
                   state=rep["view"]["ports/1/state"], moved=bool(moved))
        packets = delta(rep["view"], rep["view1"], "counters/port_xmit_packets") if rep["view1"] else None
        data = delta(rep["view"], rep["view1"], "counters/port_xmit_data") if rep["view1"] else None
        row["minBPP"] = row["maxBPP"] = 0
        if packets and packets >= MIN_TRAFFIC_PACKETS and data:
            bpp = data * 4 / packets
            if layer == "InfiniBand":
                row["maxBPP"] = INFINIBAND_MAX_BYTES_PER_PACKET
                if bpp >= BULK_BYTES_PER_PACKET:
                    row["minBPP"] = BULK_BYTES_PER_PACKET
            else:
                row["minBPP"] = ETHERNET_MIN_BYTES_PER_PACKET
            row["bpp"] = bpp
        rows.append(row)

    write_go(generated, rows)
    for r in rows:
        extra = f" bpp={r['bpp']:.0f} [{r['minBPP']},{r['maxBPP']}]" if "bpp" in r else ""
        print(f"{r['file']}: ports={r['ports']} unc={len(r['uncataloged'])} unreadable={len(r['unreadable'])} saturated={len(r['saturated'])} moved={r['moved']}{extra}")


def go_strings(items):
    return "nil" if not items else "[]string{" + ", ".join(f'"{s}"' for s in items) + "}"


def write_go(path, rows):
    with open(path, "w", encoding="utf-8") as f:
        f.write("// Code generated by testdata/rdmasysfs/fleet-export.py; DO NOT EDIT.\n\n")
        f.write("package linkmonitor\n\n")
        f.write("// rdmaFleetFixtures is one row per testdata/rdmasysfs/fleet fixture.\n")
        f.write("var rdmaFleetFixtures = []rdmaFleetFixture{\n")
        for r in rows:
            category, what = "positive", f"{r['model']} {r['layer']} port ({r['state']}), as on {r['ports']} fleet port{'s' if r['ports'] != 1 else ''}"
            expected = "every cataloged file is one sample at its scale, plus the rate"
            if r["unreadable"]:
                category = "corner"
                what += f", {len(r['unreadable'])} counters unreadable"
                expected = "supported: the unreadable files are skipped and pruned from the schema, the rest are samples"
            elif r["saturated"]:
                category = "boundary"
                what += f", {' '.join(r['saturated'])} saturated"
                expected = "the saturated register is reported exactly, not wrapped or dropped"
            f.write("\t{\n")
            f.write(f'\t\tfile: "{r["file"]}", category: "{category}",\n')
            f.write(f'\t\tdescription: "{what}",\n')
            f.write(f'\t\texpected: "{expected}",\n')
            f.write(f"\t\trate: {r['rate']},\n")
            if r["minBPP"] or r["maxBPP"]:
                f.write(f"\t\tminBytesPerPacket: {r['minBPP']}, maxBytesPerPacket: {r['maxBPP']},\n")
            for field in ("uncataloged", "unreadable", "saturated"):
                if r[field]:
                    f.write(f"\t\t{field}: {go_strings(r[field])},\n")
            f.write("\t},\n")
        f.write("}\n")
    subprocess.run(["gofmt", "-w", path], check=True)


if __name__ == "__main__":
    main()
