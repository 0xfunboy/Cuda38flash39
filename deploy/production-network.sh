#!/usr/bin/env bash
# Add production LAN addresses and an independent SSH listener, never replace 22.
# Run explicitly with sudo on each EVO while eno1 is disconnected.
set -Eeuo pipefail
umask 077
export LC_ALL=C
[[ $EUID == 0 ]] || { echo 'Run this script with sudo on each EVO.' >&2; exit 1; }
task_deploy=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
case "$(hostname)" in
  01-EVO-X3) task_node=node01; task_port=7777; task_address=192.168.178.77/24; task_mac=00:84:97:1a:9c:2e ;;
  02-EVO-X3) task_node=node02; task_port=9999; task_address=192.168.178.67/24; task_mac=00:84:97:1a:9b:32 ;;
  *) echo 'Unexpected host; no changes made.' >&2; exit 2 ;;
esac
for task_tool in netplan nmcli systemctl ss ssh-keyscan awk install cmp; do
  command -v "$task_tool" >/dev/null
done
[[ $(tr '[:upper:]' '[:lower:]' </sys/class/net/eno1/address) == "$task_mac" ]] || {
  echo 'Ethernet identity mismatch; no changes made.' >&2; exit 2;
}
[[ $(nmcli -g GENERAL.STATE device show eno1) != 100* ]] || {
  echo 'Ethernet is active. Stage this change in a separate network maintenance window.' >&2; exit 2;
}
systemctl is-active --quiet ssh.service
systemctl is-active --quiet NetworkManager.service
task_ssh_id=$(systemctl show ssh.service -p InvocationID --value)
task_wifi=$(nmcli -g IP4.ADDRESS device show wlp99s0)
task_usb=$(nmcli -g IP4.ADDRESS device show thunderbolt0)
[[ -n "$task_wifi" && -n "$task_usb" ]] || { echo 'Expected Wi-Fi/USB4 addresses missing.' >&2; exit 2; }
task_primary_key=$(ssh-keyscan -T 5 -t ed25519 -p 22 127.0.0.1 2>/dev/null | awk '$2 == "ssh-ed25519" {print $2, $3}')
[[ -n "$task_primary_key" ]] || { echo 'Port 22 host-key check failed.' >&2; exit 2; }
task_unit="haloclu-ssh-extra@$task_port.service"
task_unit_file=/etc/systemd/system/haloclu-ssh-extra@.service
task_netplan=/etc/netplan/99-haloclu-production-lan.yaml
task_template="$task_deploy/network/$task_node.yaml"
[[ -f "$task_template" && -f "$task_deploy/haloclu-ssh-extra@.service" ]]
# Refuse to overwrite foreign configuration or disturb an already-open listener.
for task_pair in "$task_netplan|$task_template" "$task_unit_file|$task_deploy/haloclu-ssh-extra@.service"; do
  task_dest=${task_pair%%|*}; task_source=${task_pair#*|}
  if [[ -e "$task_dest" || -L "$task_dest" ]]; then
    [[ ! -L "$task_dest" ]] && cmp -s "$task_dest" "$task_source" || {
      echo "Existing different configuration: $task_dest. No changes made." >&2; exit 2;
    }
  fi
done
task_open=$(ss -H -ltn "sport = :$task_port")
if [[ -n "$task_open" ]]; then
  systemctl is-active --quiet "$task_unit" && [[ -f "$task_netplan" && -f "$task_unit_file" ]] || {
    echo 'Requested port is already in use by an unverified listener.' >&2; exit 2;
  }
  echo 'This configuration is already installed. No changes made.'
  exit 0
fi
install -d -m 700 /var/lib/haloclu
task_receipt=$(mktemp -d /var/lib/haloclu/network-XXXXXXXX)
nmcli -f connection,ipv4,ipv6 connection show netplan-eno1 > "$task_receipt/ethernet-before.txt"
ip -brief address > "$task_receipt/addresses-before.txt"
systemctl cat ssh.service ssh.socket > "$task_receipt/primary-ssh-before.txt"
task_new_netplan=0; task_new_unit=0
[[ -e "$task_netplan" ]] || task_new_netplan=1
[[ -e "$task_unit_file" ]] || task_new_unit=1
task_started=0; task_ufw_rule=0
rollback() {
  task_rc=$?
  trap - ERR
  set +e
  [[ $task_started == 0 ]] || systemctl disable --now "$task_unit"
  [[ $task_ufw_rule == 0 ]] || ufw --force delete allow "$task_port/tcp"
  [[ $task_new_unit == 0 ]] || unlink "$task_unit_file"
  [[ $task_new_netplan == 0 ]] || unlink "$task_netplan"
  netplan generate > "$task_receipt/rollback-generate.txt" 2>&1
  nmcli connection reload
  systemctl daemon-reload
  echo "Setup failed; added configuration rolled back. Receipt: $task_receipt" >&2
  exit "$task_rc"
}
trap rollback ERR
install -m 600 "$task_template" "$task_netplan"
netplan generate > "$task_receipt/netplan-generate.txt" 2>&1
# Reload profile definitions only: no netplan apply, device reapply, Wi-Fi down,
# NetworkManager restart, default SSH restart, or inference lifecycle operations.
nmcli connection reload
[[ $(nmcli -g ipv4.method connection show netplan-eno1) == auto ]]
nmcli -g ipv4.addresses connection show netplan-eno1 | grep -Fx "$task_address" >/dev/null
[[ $(nmcli -g IP4.ADDRESS device show wlp99s0) == "$task_wifi" ]]
[[ $(nmcli -g IP4.ADDRESS device show thunderbolt0) == "$task_usb" ]]
install -m 644 "$task_deploy/haloclu-ssh-extra@.service" "$task_unit_file"
systemctl daemon-reload
if command -v ufw >/dev/null && ufw status | grep -q '^Status: active'; then
  ufw status > "$task_receipt/ufw-before.txt"
  # Do not remove a pre-existing identical rule on rollback.
  if ! ufw status | awk -v p="$task_port/tcp" '$1 == p && $2 == "ALLOW" && $3 == "Anywhere" {found=1} END {exit !found}'; then
    ufw allow "$task_port/tcp" comment 'HaloClu additional SSH'
    task_ufw_rule=1
  fi
fi
task_started=1
systemctl enable --now "$task_unit"
task_extra_key=''
for task_attempt in 1 2 3 4 5; do
  task_extra_key=$(ssh-keyscan -T 3 -t ed25519 -p "$task_port" 127.0.0.1 2>/dev/null | awk '$2 == "ssh-ed25519" {print $2, $3}') || true
  [[ -z "$task_extra_key" ]] || break
  sleep 1
done
[[ "$task_extra_key" == "$task_primary_key" ]]
[[ $(systemctl show ssh.service -p InvocationID --value) == "$task_ssh_id" ]]
[[ $(nmcli -g IP4.ADDRESS device show wlp99s0) == "$task_wifi" ]]
[[ $(nmcli -g IP4.ADDRESS device show thunderbolt0) == "$task_usb" ]]
ss -H -ltn 'sport = :22' | grep -q .
ip -brief address > "$task_receipt/addresses-after.txt"
nmcli -f connection,ipv4,ipv6 connection show netplan-eno1 > "$task_receipt/ethernet-after.txt"
systemctl status "$task_unit" --no-pager > "$task_receipt/extra-ssh.txt"
trap - ERR
printf 'PASS %s: eno1 staged for DHCP + %s; SSH22 retained, SSH%s added.\n' "$task_node" "$task_address" "$task_port"
printf 'Wi-Fi and USB4 addresses unchanged. Receipt: %s\n' "$task_receipt"
printf '%s\n' 'Ethernet addresses become active when the cable/profile connects. Reserve the static IP on the router; LAN reachability has not been tested.'
