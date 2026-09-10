# Production LAN and additional SSH ports

This configuration is specific to the two EVO-X3 hosts. It adds Ethernet
addresses while retaining DHCP, existing Wi-Fi configuration and the private
USB4 inference link.

| Host | Ethernet | Additional static address | Ethernet MAC | SSH ports |
|---|---|---|---|---|
| 01-EVO-X3 | eno1 | 192.168.178.77/24 | 00:84:97:1a:9c:2e | 22 and 7777 |
| 02-EVO-X3 | eno1 | 192.168.178.67/24 | 00:84:97:1a:9b:32 | 22 and 9999 |

The `/24` subnet is an explicit assumption. Confirm it against the production
router before applying. Reserve/exclude `.77` and `.67` in the router's DHCP
configuration so they cannot be leased to another device. DHCP remains enabled
on Ethernet and supplies automatic network settings; no gateway or DNS address
is hard-coded. The current Wi-Fi DHCP addresses are not edited. When Ethernet
connects, NetworkManager can select its DHCP route as the preferred route.

## Apply on each host

The installer needs root. Stage the configuration while `eno1` is disconnected:

```bash
sudo bash /home/funboy/StrixHaloClusterGLM/deploy/production-network.sh
```

The script checks hostname and Ethernet MAC, refuses conflicting configuration
or an occupied extra port, and installs a small Netplan override. It runs
`netplan generate` and reloads profile definitions, **not** `netplan apply`.
The addresses take effect when Ethernet connects. It does not cycle Wi-Fi,
restart NetworkManager, alter USB4, restart the model or reboot the machines.

SSH uses an independent `haloclu-ssh-extra@7777.service` or
`haloclu-ssh-extra@9999.service`, with the existing OpenSSH configuration, host
keys and authentication policy. The primary `ssh.service`/`ssh.socket` and
port22 remain unchanged. Explicit listen addresses prevent the additional
daemon from binding port22. No password or root-login policy is relaxed.

If UFW is active, the installer adds the requested TCP allow rule. It does not
enable/disable the firewall or rewrite custom nftables rules. Custom firewalls,
router rules and remote reachability need separate verification. Changing SSH
ports is not a substitute for authentication or firewall policy.

The installer verifies DHCP plus the static address in the stored Ethernet
profile, unchanged Wi-Fi/USB4 addresses, the same primary SSH invocation, and
the same Ed25519 host key on the new port. Failure rolls back newly installed
configuration. Root-only receipts are stored under `/var/lib/haloclu/network-*`.

## Connect and verify

From another machine on the production LAN, after connecting the cables:

```bash
ssh -p 7777 funboy@192.168.178.77
ssh -p 9999 funboy@192.168.178.67
# Standard SSH remains available:
ssh funboy@192.168.178.77
ssh funboy@192.168.178.67
```

Before the move, the extra ports can be tested using the machines' current
Wi-Fi addresses after the installer succeeds. Do not replace the internal
`02-evo-x3-tb` alias or its USB4 address with either LAN address.

On each host:

```bash
nmcli -f ipv4.method,ipv4.addresses connection show netplan-eno1
ip -brief address show eno1
ss -lnt
```

The offline validation checks syntax and the generated NetworkManager profiles.
It does not prove a free production IP, a successful root installation, a real
reboot, remote SSH login or LAN reachability. The initial delivery requires the
owner's interactive sudo command; it was not applied automatically.

## Remove the additions

Keep access through port22 and disconnect Ethernet before reversing its staged
configuration. Disable only this host's extra listener (7777 or9999):

```bash
sudo systemctl disable --now haloclu-ssh-extra@7777.service  # NODE01
# NODE02 uses haloclu-ssh-extra@9999.service instead.
sudo unlink /etc/netplan/99-haloclu-production-lan.yaml
sudo netplan generate
sudo nmcli connection reload
```

Remove only an installer-added UFW rule if applicable; the receipt contains the
previous UFW state. The Netplan source files remain in this directory for
reinstallation. Headless boot is a separate explicit step described in
[cluster operations](../../runtime/OPERATIONS.md).
