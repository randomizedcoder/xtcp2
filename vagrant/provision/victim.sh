#!/usr/bin/env bash
# vagrant/provision/victim.sh — runs as root inside the victim VM, after
# common.sh. Materializes the "prod host" the scenarios attack:
#
#   1. the catcher (TCP jsonl sink the daemon's -dest tcp: writes into — the
#      RT-11 "records still arriving" assertion consumes it host-side, NOT a
#      daemon gauge, so an attacker who blinds the daemon cannot also forge it)
#   2. the xtcp2 daemon container in the deployment-matrix shape
#   3. the RT-11 work population (tcp_server/tcp_client containers — the
#      non-attacker tenants whose records must keep arriving)
#   4. the co-tenant attacker container at the requested tier
#
# Matrix args come from the Vagrantfile (XTCP2_* env; the scenario apps in
# nix/redteam.nix set and validate them). Idempotent: containers are removed
# and re-created on every provision.
#
# The scenario app scp's the image tarballs to /tmp/xtcp2-redteam/ BEFORE
# `vagrant provision` runs (vagrant/lib/ssh.sh); there is no synced folder
# (the box image ships no rsync), so that scp is the only delivery path.
set -euo pipefail

MODE="$1"
AUTH="$2"
CAPS="$3"
ATTACKER_TIER="$4"

die() {
  echo "victim.sh: $*" >&2
  exit 1
}

case "$MODE" in
  host | bridge) ;;
  *) die "invalid XTCP2_MODE '$MODE' (want host|bridge)" ;;
esac
case "$AUTH" in
  off | raw | hmac) ;;
  *) die "invalid XTCP2_AUTH '$AUTH' (want off|raw|hmac)" ;;
esac
case "$CAPS" in
  priv | min) ;;
  *) die "invalid XTCP2_CAPS '$CAPS' (want priv|min)" ;;
esac
case "$ATTACKER_TIER" in
  plain | read-cred | privileged | admin) ;;
  *) die "invalid XTCP2_ATTACKER '$ATTACKER_TIER'" ;;
esac

HARNESS_DIR=/home/vagrant/xtcp2-redteam
DAEMON_IMAGE=xtcp2:latest
STRESS_IMAGE=xtcp2-tcp-stress:latest

# Each `docker load` of a rebuilt image leaves the previous layers in
# containerd (reclaimed only by its delayed GC), and 10G disks fill across
# re-provision cycles. Prune first: images with a running container stay.
docker image prune -af >/dev/null

[ -f "$HARNESS_DIR/xtcp2-oci.tar.gz" ] \
  || die "$HARNESS_DIR/xtcp2-oci.tar.gz missing — run via nix run .#redteam-* (it scp's the tarballs before provisioning)"
docker load <"$HARNESS_DIR/xtcp2-oci.tar.gz"
[ -f "$HARNESS_DIR/xtcp2-tcp-stress.tar.gz" ] \
  || die "$HARNESS_DIR/xtcp2-tcp-stress.tar.gz missing — run via nix run .#redteam-*"
docker load <"$HARNESS_DIR/xtcp2-tcp-stress.tar.gz"

# --- catcher ----------------------------------------------------------------
# socat appends every connection's bytes to records.jsonl. The daemon connects
# from its container netns; host mode dials 127.0.0.1, bridge mode the docker0
# gateway 172.17.0.1 — socat binds 0.0.0.0, so both reach it.
mkdir -p /var/lib/xtcp2-catcher
# socat's OPEN:...append does not O_CREAT — the file must exist before the unit
# starts, or every connection's child exits with ENOENT and the daemon's
# envelope writes land on a broken pipe.
touch /var/lib/xtcp2-catcher/records.jsonl
cat >/etc/systemd/system/xtcp2-catcher.service <<'EOF'
[Unit]
Description=xtcp2 redteam catcher (TCP jsonl sink)
After=network.target

[Service]
# SYSTEM:'cat >> file', not OPEN:file: OPEN reads the plain file's EOF
# immediately, so each fork child exits after socat's 0.5s poll timeout and
# the daemon's single-shot TCP connection (pkg/xtcp/destinations_tcp.go dials
# once, never reconnects) lands on a broken pipe within seconds.
ExecStart=/usr/bin/socat TCP-LISTEN:9999,reuseaddr,fork SYSTEM:'cat >> /var/lib/xtcp2-catcher/records.jsonl'
Restart=always

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now xtcp2-catcher
systemctl restart xtcp2-catcher

# --- secrets (auth rows) ----------------------------------------------------
mkdir -p /opt/xtcp2-redteam/secrets
if [ "$AUTH" != off ] && [ ! -f /opt/xtcp2-redteam/secrets/read ]; then
  # Generated once per VM, 0400 root-only (the daemon refuses group/other
  # readable credential files with exit 64). Scenarios read them host-side via
  # `vagrant ssh victim -c 'sudo cat …'` when a vantage needs a real token.
  umask 077
  openssl rand -hex 32 >/opt/xtcp2-redteam/secrets/read
  openssl rand -hex 32 >/opt/xtcp2-redteam/secrets/admin
  chmod 0400 /opt/xtcp2-redteam/secrets/read /opt/xtcp2-redteam/secrets/admin
fi

# --- daemon container -------------------------------------------------------
NET_ARGS=()
case "$MODE" in
  # host: the TODO-SOON §29 prod shape (build/containers/docker-compose.yml).
  host)
    NET_ARGS=(--network host --pid host --ipc host)
    DEST_ADDR=127.0.0.1
    ;;
  # bridge: ports published on the VM host; the daemon dials the catcher via
  # the docker0 gateway.
  bridge)
    NET_ARGS=(-p 9088:9088 -p 8889:8889)
    DEST_ADDR=172.17.0.1
    ;;
esac

CAP_ARGS=()
case "$CAPS" in
  # priv: docker's defaults DROP NET_ADMIN/SYS_ADMIN — the §29 prod shape
  # (build/containers/docker-compose.yml) adds exactly these three.
  priv)
    CAP_ARGS=(--cap-add=NET_ADMIN --cap-add=SYS_ADMIN --cap-add=SYS_CHROOT)
    ;;
  # min: the daemon's own startup check remediation (pkg/xtcp/init_capabilities.go
  # + the exit-64 text): the recommended AmbientCapabilities set, nothing more.
  # rt-5-4 refines per-feature; this is its Phase 0 baseline row.
  min)
    CAP_ARGS=(
      --cap-drop=ALL
      --cap-add=NET_ADMIN
      --cap-add=SYS_ADMIN
      --cap-add=SYS_PTRACE
      --cap-add=NET_RAW
      --cap-add=SYS_RESOURCE
    )
    ;;
esac

# Listener specs — the -listener flag shape nix/tests/xtcp2-scrape-auth.nix
# uses. The current daemon (the listener-security-hardening program, PRs
# #291/#300) refuses to start on an unauthenticated non-loopback TCP
# listener, so the matrix's AUTH rows are:
#
#   off  — the one legal unauthenticated shape (docs/security.md "listeners"):
#          loopback TCP with auth=disabled. The red-team point of the row: a
#          co-tenant container must NOT be able to reach it (loopback is the
#          host netns' 127.0.0.1, the co-tenant is another netns) → BLOCKED.
#   raw  — the TODO-SOON §29 prod shape: :9088/:8889 inheriting the mounted
#          read/admin token files (an unset mode with a tokenFile is raw).
#          No token → 401 (BLOCKED); a stolen read token → 200 (the
#          read-cred tier's CONFIRMED exposure).
LISTENER_ARGS=()
SECRET_MOUNTS=()
case "$AUTH" in
  off)
    LISTENER_ARGS=(
      -listener "name=prometheus,serves=health+metrics+metrics-detail,address=127.0.0.1:9088,network=tcp,auth=disabled"
      -listener "name=grpc,serves=grpc-read,address=127.0.0.1:8889,network=tcp,auth=disabled"
    )
    ;;
  raw)
    LISTENER_ARGS=(
      -listener "name=prometheus,serves=health+metrics+metrics-detail,address=:9088,network=tcp,tokenFile=/run/secrets/read,adminTokenFile=/run/secrets/admin"
      -listener "name=grpc,serves=grpc-read+grpc-admin,address=:8889,network=tcp,tokenFile=/run/secrets/read,adminTokenFile=/run/secrets/admin"
    )
    SECRET_MOUNTS=(-v /opt/xtcp2-redteam/secrets:/run/secrets:ro)
    ;;
  hmac)
    LISTENER_ARGS=(
      -listener "name=prometheus,serves=health+metrics+metrics-detail,address=:9088,network=tcp,hmacKeyFile=/run/secrets/read,adminHmacKeyFile=/run/secrets/admin"
      -listener "name=grpc,serves=grpc-read+grpc-admin,address=:8889,network=tcp,hmacKeyFile=/run/secrets/read,adminHmacKeyFile=/run/secrets/admin"
    )
    SECRET_MOUNTS=(-v /opt/xtcp2-redteam/secrets:/run/secrets:ro)
    ;;
esac

docker rm -f rt-xtcp2 >/dev/null 2>&1 || true
docker run -d --name rt-xtcp2 \
  "${NET_ARGS[@]}" \
  "${CAP_ARGS[@]}" \
  "${SECRET_MOUNTS[@]}" \
  -v /var/lib/xtcp2:/var/lib/xtcp2 \
  -v /run/netns:/run/netns:ro,rslave \
  -v /run/docker/netns:/run/docker/netns:ro,rslave \
  --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:ro \
  "$DAEMON_IMAGE" \
  "${LISTENER_ARGS[@]}" \
  -dest "tcp:$DEST_ADDR:9999" \
  -marshal jsonl \
  -frequency 2s \
  -timeout 1s \
  -hostname rt-victim

# --- RT-11 work population -------------------------------------------------
# Two non-attacker tenant containers; their records must keep arriving at the
# catcher within the poll cadence — the pass criterion of every RT-11 attack.
for i in 1 2; do
  docker rm -f "rt-work-$i" >/dev/null 2>&1 || true
  docker run -d --name "rt-work-$i" --network bridge \
    -e TCP_MODE=both -e TCP_COUNT=25 -e TCP_SLEEP=5s \
    "$STRESS_IMAGE"
done

# --- co-tenant attacker container ------------------------------------------
# plain: an ordinary container on the default bridge — the RT-11 tier 1
# vantage: reach the host network stack, nothing else. Higher tiers add
# stolen credentials, privileges, the docker socket (Phase 2/5 scenarios).
docker rm -f rt-attacker >/dev/null 2>&1 || true
case "$ATTACKER_TIER" in
  plain)
    docker run -d --name rt-attacker --network bridge \
      ubuntu:24.04 sleep infinity
    ;;
  read-cred)
    docker run -d --name rt-attacker --network bridge \
      -v /opt/xtcp2-redteam/secrets/read:/run/secrets/read:ro \
      ubuntu:24.04 sleep infinity
    ;;
  admin)
    docker run -d --name rt-attacker --network bridge \
      -v /opt/xtcp2-redteam/secrets:/run/secrets:ro \
      ubuntu:24.04 sleep infinity
    ;;
  privileged)
    docker run -d --name rt-attacker --network bridge \
      --privileged --pid host \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -v /opt/xtcp2-redteam/secrets:/run/secrets:ro \
      ubuntu:24.04 sleep infinity
    ;;
esac

# Pre-warm the attack tool so the scenario's docker exec is deterministic.
docker exec rt-attacker bash -c 'apt-get update -qq && apt-get install -y -qq curl'

docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
echo "victim: MODE=$MODE AUTH=$AUTH CAPS=$CAPS ATTACKER=$ATTACKER_TIER dest=tcp:$DEST_ADDR:9999"
