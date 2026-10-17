#!/usr/bin/env bash
# Reports whether each piece of the local dev stack (started by up.sh) is
# currently running.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
source scripts/dev/lib.sh

report() {
	local name="$1" pidfile="$DEVLOGS/$1.pid" port="$2"
	if pid_alive "$pidfile" && tcp_open 127.0.0.1 "$port"; then
		echo "  ✓ $name — running (pid $(cat "$pidfile"), port $port)"
	elif pid_alive "$pidfile"; then
		echo "  ! $name — process alive (pid $(cat "$pidfile")) but port $port not open yet"
	else
		echo "  ✗ $name — not running"
	fi
}

echo "── Dev stack status ──────────────────────────────────────────────"
# up.sh reuses whatever's already listening on the port, not necessarily
# the sarvex-dev-mongo/redis containers it would otherwise create — so port
# occupancy, not container name, is the real signal here.
if tcp_open 127.0.0.1 "$MONGO_HOST_PORT"; then
	echo "  ✓ mongo — reachable on port $MONGO_HOST_PORT"
else
	echo "  ✗ mongo — not reachable on port $MONGO_HOST_PORT"
fi
if tcp_open 127.0.0.1 "$REDIS_HOST_PORT"; then
	echo "  ✓ redis — reachable on port $REDIS_HOST_PORT"
else
	echo "  ✗ redis — not reachable on port $REDIS_HOST_PORT"
fi
if tcp_open 127.0.0.1 "$NATS_HOST_PORT"; then
	echo "  ✓ nats — reachable on port $NATS_HOST_PORT"
else
	echo "  ✗ nats — not reachable on port $NATS_HOST_PORT"
fi
if tcp_open 127.0.0.1 "$VAULT_HOST_PORT"; then
	echo "  ✓ vault — reachable on port $VAULT_HOST_PORT"
else
	echo "  ✗ vault — not reachable on port $VAULT_HOST_PORT"
fi
if tcp_open 127.0.0.1 "$OTEL_GRPC_HOST_PORT"; then
	echo "  ✓ otel-collector — reachable on port $OTEL_GRPC_HOST_PORT"
else
	echo "  ✗ otel-collector — not reachable on port $OTEL_GRPC_HOST_PORT"
fi
report sim "$SIM_PORT"
if pid_alive "$DEVLOGS/monolith.pid"; then
	# up-monolith.sh: one sarvex --service=all process hosts all three
	# planes, so there's no separate control/sync/serving pidfile to check
	# — report the one process against each of its three ports instead.
	monolith_pid="$(cat "$DEVLOGS/monolith.pid")"
	for plane_port in "control:$CONTROL_PORT" "sync:$SYNC_PORT" "serving:$SERVING_PORT"; do
		plane="${plane_port%%:*}" port="${plane_port##*:}"
		if tcp_open 127.0.0.1 "$port"; then
			echo "  ✓ $plane — running (pid $monolith_pid, port $port, monolith)"
		else
			echo "  ! $plane — monolith process alive (pid $monolith_pid) but port $port not open yet"
		fi
	done
else
	report control "$CONTROL_PORT"
	report sync "$SYNC_PORT"
	report serving "$SERVING_PORT"
fi
