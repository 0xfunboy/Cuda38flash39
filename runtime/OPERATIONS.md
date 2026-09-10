# Operating HaloClu on two EVO-X3 hosts

## Production layout

| Component | Location |
|---|---|
| Gateway, frontend, Pi and management | `/home/funboy/StrixHaloClusterGLM` |
| Qualified CIRU/vLLM/ROCm environment | `StrixHaloClusterGLM/.engine` on both hosts |
| GLM target shards and DFlash2 weights | `/home/funboy/models/ciru-glm53-flash` on both hosts |
| Other retained model checkpoints | `/home/funboy/models` |
| API credentials, sessions, conversations, pair ownership | `StrixHaloClusterGLM/state` |
| Frozen operational test fixtures | `StrixHaloClusterGLM/runtime/fixtures` |
| Research archives | `StrixHaloClusterGLM/archives/research-node0N-20260910.tar.zst` on each host |

The numerical preset remains W4, TP2/PP1, DFlash2 k5/local0, Socket over USB4,
prefix cache off, 64K engine profile. All five correctness fixes and the
operational JIT caches remain installed. No model download is required.

## Start, stop and restart

Run on NODE01 as `funboy`:

```bash
systemctl --user start strixglm.service
systemctl --user restart haloclu-engine.service
systemctl --user stop haloclu-engine.service
```

The dependency order drains the gateway and coordinator before stopping the
entire owned pair. Start/restart manages both ranks; never restart an individual
rank. Startup waits up to five minutes for the USB4 peer; model loading also has
a bounded deadline. A foreign/replaced unit fails ownership validation instead
of being taken over. Failed inference requests are not automatically replayed.

```bash
systemctl --user status haloclu-engine.service strixglm-pair.service strixglm.service
cd /home/funboy/StrixHaloClusterGLM
./bin/strixglm cluster status
journalctl --user -u haloclu-engine.service -u strixglm-pair.service -u strixglm.service -n 100
```

Rank logs and current ownership are under `state/cluster`. Each boot uses fresh
unit identities. Old snapshots are historical, not live ownership. To revert a
future product edit, restore its saved configuration/binary and use this same
whole-pair lifecycle. Archived old-path restore commands cannot be run against
the relocated installation without first restoring their archived layout.

## Headless next boot

User lingering and NODE01 API startup units are enabled. Cluster SSH works
without a graphical key agent. Run with sudo on **each** host:

```bash
sudo bash /home/funboy/StrixHaloClusterGLM/deploy/headless-next-boot.sh
```

This selects `multi-user.target` and disables future startup of printer,
Bluetooth, modem and mDNS services. It does not terminate the current desktop
session or reboot. SSH, networking/USB4, bolt, GPU drivers, time synchronization
and security updates are retained. The cleanup agent could not execute this
privileged step because sudo requires interactive authentication. Until the
owner runs it, the default boot target remains graphical.

## API exposure

For Ethernet DHCP plus the requested static LAN addresses and additional SSH
listeners, use [Production Ethernet and SSH](../deploy/network/README.md).
The installer leaves Wi-Fi, port22 and the USB4 rank link unchanged; it requires
an explicit root invocation on each host. Do not use LAN addresses for RCCL.

HaloClu remains bound to `127.0.0.1:18093`; the paired coordinator is loopback18094
and rank APIs use private USB4 addresses, port18110. There is no new public
listener. Before connecting a domain/public IP, configure authenticated HTTPS
ingress and firewall the internal rank/RCCL ports. Public-domain deployment has
not been performed by this cleanup.

## Research recovery

History remains `/home/funboy/STRIX_CLUSTER_ACCELERATION_PLAN.md`. Historical
paths are now archive member names, not live dependencies. Archives retain
sources, Git metadata, configuration and raw reports. Old SDK installations,
container caches, temporary files and downloads were excluded; the live runtime
and all inference checkpoints were retained by relocation.

For a particular report on NODE01, for example:

```bash
tar -xOf archives/research-node01-20260910.tar.zst ai-exp/reports/moe-cluster/GLM-M6-OPT-002/FINAL.md
```

Archive manifests, exclusions, checksums and deletion receipts are in
`reports/SYSTEM-CLEANUP-001`. Archives may contain private configuration/model
outputs: they are permission-restricted and excluded from Git. They are local
recovery copies, not off-machine backups. Extract into a separate recovery
directory, never over the live product/models. Git worktrees may require their
archived parent repository metadata as well as the worktree sources.
