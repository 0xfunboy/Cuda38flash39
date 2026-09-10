#!/usr/bin/env bash
# Boot/lifecycle wrapper around the existing ownership-aware ALL-RANK controller.
set -Eeuo pipefail
cd /home/funboy/StrixHaloClusterGLM
[[ "$(hostname)" == 01-EVO-X3 ]] || exit 2
task_action=${1:?start or stop}
task_cli=(/home/funboy/StrixHaloClusterGLM/bin/strixglm cluster --config /home/funboy/StrixHaloClusterGLM/config.json)
case "$task_action" in
  stop) exec "${task_cli[@]}" stop ;;
  start) ;;
  *) exit 2 ;;
esac
# USB4 and the peer may boot later than this user manager. No rank launches until
# the peer is reachable with the dedicated key, without any desktop ssh-agent.
task_deadline=$((SECONDS+300))
until ssh -o IdentityAgent=none -o BatchMode=yes -o ConnectTimeout=5 02-evo-x3-tb true; do
  ((SECONDS < task_deadline)) || { echo 'USB4 peer boot deadline exceeded' >&2; exit 1; }
  sleep 3
done
task_status=$("${task_cli[@]}" status)
if jq -e '
  . as $s | .owner.state == "ready" and .poisoned == false and
  (.owner.units | length) == 2 and
  all(.owner.units[]; . as $o |
    any($s.units[]; .name == $o.name and .rank == $o.rank and
      .state.ActiveState == "active" and .state.InvocationID == $o.invocation_id)) and
  all(.units[] | select(.name | startswith("ciru-")); .state.ActiveState != "active")
' <<< "$task_status" >/dev/null; then
  "${task_cli[@]}" verify
  curl --fail --silent --max-time 5 http://10.55.0.1:18110/health >/dev/null
  curl --fail --silent --max-time 5 http://10.55.0.2:18110/health >/dev/null
  echo 'Adopted the already healthy, identically owned whole pair'
  exit 0
fi
# Reconcile a stale post-reboot owner or partial owned pair. These operations
# validate both InvocationIDs/nonces; foreign services are never taken over.
"${task_cli[@]}" stop
exec "${task_cli[@]}" start
