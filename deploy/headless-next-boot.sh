#!/usr/bin/env bash
# Run with sudo on EACH EVO. Does not stop this graphical session or reboot.
set -Eeuo pipefail

disable_optional_boot_units() {
    local task_unit task_load task_state
    # Use systemd properties, not a text-search tool from the invoking user's PATH.
    # In particular, sudo need not have ripgrep or a VS Code extension installed.
    for task_unit in cups.service cups.socket cups.path cups-browsed.service bluetooth.service ModemManager.service avahi-daemon.service avahi-daemon.socket; do
        task_load=$(systemctl show --property=LoadState --value "$task_unit") || return
        case "$task_load" in
            not-found) printf 'Not installed, skipped: %s\n' "$task_unit"; continue ;;
            loaded|masked) ;;
            *) printf 'Cannot determine unit state for %s: %s\n' "$task_unit" "$task_load" >&2; return 1 ;;
        esac
        # No --now: current graphical sessions and their services remain running.
        systemctl disable "$task_unit" || return
        task_state=$(systemctl show --property=UnitFileState --value "$task_unit") || return
        case "$task_state" in
            disabled|masked|static|indirect) ;;
            *) printf 'Startup disable not verified for %s: %s\n' "$task_unit" "$task_state" >&2; return 1 ;;
        esac
    done
}

main() {
    [[ $EUID == 0 ]] || { echo 'Run with sudo on each EVO-X3' >&2; return 1; }
    case "$(hostname)" in 01-EVO-X3|02-EVO-X3) ;; *) return 2 ;; esac
    systemctl is-active --quiet ssh.service || systemctl is-active --quiet ssh.socket
    systemctl set-default multi-user.target
    disable_optional_boot_units
    [[ $(systemctl get-default) == multi-user.target ]]
    printf '%s\n' 'Headless target and peripheral startup configuration verified for next boot.'
    printf '%s\n' 'No reboot or current-session stop performed; active-trigger warnings are expected.'
    printf '%s\n' 'SSH, USB4/NetworkManager, bolt, time sync, GPU drivers and security updates retained.'
}

# Sourcing is read-only and allows regression tests with a mock systemctl.
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
    main
fi
