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

HARNESS_DIR=/tmp/xtcp2-redteam
DAEMON_IMAGE=xtcp2:latest
STRESS_IMAGE=xtcp2-tcp-stress:latest

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
cat >/etc/systemd/system/xtcp2-catcher.service <<'EOF'
[Unit]
Description=xtcp2 redteam catcher (TCP jsonl sink)
After=network.target

[Service]
ExecStart=/usr/bin/socat TCP-LISTEN:9999,reuseaddr,fork OPEN:/var/lib/xtcp2-catcher/records.jsonl,append
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
  # priv: docker defaults — everything the daemon might need (rt-5-3's blast
  # radius tier). min: exactly the startup capability check's required set
  # (pkg/xtcp/init_capabilities.go), the rt-5-4 recipe's Phase 0 approximation.
  min)
    CAP_ARGS=(--cap-drop=ALL --cap-add=NET_ADMIN --cap-add=SYS_ADMIN)
    ;;
esac

AUTH_ENVS=()
SECRET_MOUNTS=()
case "$AUTH" in
  off) ;;
  raw)
    AUTH_ENVS=(
      -e LISTENER_AUTH_MODE=raw
      -e LISTENER_RAW_TOKEN_FILE=/run/secrets/read
      -e LISTENER_ADMIN_AUTH_MODE=raw
      -e LISTENER_ADMIN_RAW_TOKEN_FILE=/run/secrets/admin
    )
    SECRET_MOUNTS=(-v /opt/xtcp2-redteam/secrets:/run/secrets:ro)
    ;;
  hmac)
    AUTH_ENVS=(
      -e LISTENER_AUTH_MODE=hmac-utc-minute
      -e LISTENER_HMAC_SHARED_KEY_FILE=/run/secrets/read
      -e LISTENER_ADMIN_AUTH_MODE=hmac-utc-minute
      -e LISTENER_ADMIN_HMAC_SHARED_KEY_FILE=/run/secrets/admin
    )
    SECRET_MOUNTS=(-v /opt/xtcp2-redteam/secrets:/run/secrets:ro)
    ;;
esac

docker rm -f rt-xtcp2 >/dev/null 2>&1 || true
docker run -d --name rt-xtcp2 \
  "${NET_ARGS[@]}" \
  "${CAP_ARGS[@]}" \
  "${AUTH_ENVS[@]}" \
  "${SECRET_MOUNTS[@]}" \
  -v /var/lib/xtcp2:/var/lib/xtcp2 \
  -v /run/netns:/run/netns:ro,rslave \
  -v /run/docker/netns:/run/docker/netns:ro,rslave \
  --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:ro \
  "$DAEMON_IMAGE" \
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
