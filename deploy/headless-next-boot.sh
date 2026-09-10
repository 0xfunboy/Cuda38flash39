#!/usr/bin/env bash
# Run with sudo on EACH EVO. Does not stop this graphical session or reboot.
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo 'Run with sudo on each EVO-X3' >&2; exit 1; }
case "$(hostname)" in 01-EVO-X3|02-EVO-X3) ;; *) exit 2 ;; esac
systemctl is-active --quiet ssh.service || systemctl is-active --quiet ssh.socket
systemctl set-default multi-user.target
# Non-inference peripherals: disable future startup, not current-session units.
for task_unit in cups.service cups.socket cups.path cups-browsed.service bluetooth.service ModemManager.service avahi-daemon.service avahi-daemon.socket; do
    if systemctl list-unit-files "$task_unit" --no-legend | rg -q "^${task_unit//./\.}"; then
        systemctl disable "$task_unit"
    fi
done
printf '%s\n' 'Headless target saved for next boot. No reboot or session termination was performed.'
printf '%s\n' 'SSH, USB4/NetworkManager, bolt, time sync, GPU drivers and security updates retained.'
