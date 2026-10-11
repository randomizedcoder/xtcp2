"""External HTTP load and pprof capture helpers for go-link-monitor."""

import argparse
import json
import os
from pathlib import Path
from urllib.parse import urljoin
from urllib.request import Request, urlopen


def endpoint(base, path):
    return urljoin(base.rstrip("/") + "/", path.lstrip("/"))


def write_urls(base, path, output, metrics_weight=90, health_weight=5, ready_weight=5,
               all_weight=0, all_path="/metrics-all"):
    """Write a 100-line siege URL mix; all_weight adds full-exposition scrapes."""
    if not base.startswith(("http://", "https://")):
        raise ValueError("base URL must use http or https")
    weights = [("/metrics", metrics_weight), ("/healthz", health_weight),
               ("/readyz", ready_weight), (all_path, all_weight)]
    if any(weight < 0 for _, weight in weights) or sum(weight for _, weight in weights) != 100:
        raise ValueError("URL weights must be non-negative and sum to 100")
    path.write_text("\n".join(endpoint(base, item) for item, weight in weights for _ in range(weight)) + "\n")
    return {item: weight for item, weight in weights if weight}


def fetch_profile(base, name, output, seconds=30, timeout=60, headers=()):
    target = endpoint(base, "/debug/pprof/" + name)
    if name == "profile":
        target += "?seconds=" + str(seconds)
    request = Request(target, headers=dict(item.split(":", 1) for item in headers))
    with urlopen(request, timeout=timeout) as response:
        data = response.read()
    if len(data) < 64:
        raise ValueError(f"profile {name} is missing or truncated")
    destination = output / (name.replace("/", "-") + ".pprof")
    destination.write_bytes(data)
    return destination.stat().st_size


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default=os.environ.get("LINKMONITOR_URL"))
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--header", action="append", default=[])
    parser.add_argument("--seconds", type=int, default=30)
    parser.add_argument("--timeout", type=int, default=60)
    parser.add_argument("--profile", action="store_true")
    parser.add_argument("--trace", action="store_true")
    args = parser.parse_args()
    if not args.url:
        parser.error("--url or LINKMONITOR_URL is required")
    if args.seconds < 1 or args.timeout < 1:
        parser.error("seconds and timeout must be positive")
    args.output.mkdir(parents=True, exist_ok=False)
    manifest = {"url": args.url, "seconds": args.seconds, "timeout": args.timeout,
                "headers": args.header, "status": "incomplete"}
    (args.output / "manifest.json").write_text(json.dumps(manifest, indent=2))
    if args.profile:
        for name in ("profile", "heap", "block", "mutex"):
            fetch_profile(args.url, name, args.output, args.seconds, args.timeout, args.header)
    if args.trace:
        fetch_profile(args.url, "trace", args.output, args.seconds, args.timeout, args.header)
    manifest["status"] = "complete"
    (args.output / "manifest.json").write_text(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    main()
