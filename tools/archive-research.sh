#!/usr/bin/env bash
# SYSTEM-CLEANUP-001: preserve local research before removing old installations.
set -Eeuo pipefail
umask 077
task_product=/home/funboy/StrixHaloClusterGLM
case "$(hostname)" in
    01-EVO-X3) task_node=node01 ;;
    02-EVO-X3) task_node=node02 ;;
    *) echo 'Unexpected host' >&2; exit 2 ;;
esac
task_report="$task_product/reports/SYSTEM-CLEANUP-001"
task_archive="$task_product/archives/research-$task_node-20260910.tar.zst"
mkdir -p "$task_report" "$task_product/archives"
chmod 700 "$task_product/archives"
[[ ! -e "$task_archive" && ! -e "$task_archive.partial" ]] || exit 2
task_roots=(ai ai-exp)
[[ ! -d /home/funboy/code-gpt-main ]] || task_roots+=(code-gpt-main)
[[ ! -d /home/funboy/.config/llama-strix ]] || task_roots+=(.config/llama-strix)
[[ ! -d /home/funboy/.local/state/llama-strix ]] || task_roots+=(.local/state/llama-strix)
task_roots+=(.config/systemd/user)
# Exclusions are reproduced packages/caches, or the LIVE engine components
# preserved by relocation, never research reports, source trees or git metadata.
task_excludes=(
    ai/models ai/runtimes ai/cache ai/downloads ai/tmp
    ai-exp/models ai-exp/runtimes ai-exp/cache ai-exp/downloads ai-exp/tmp
    ai-exp/strix-ciru-tp2/artifacts ai-exp/strix-ciru-tp2/venv
    ai-exp/strix-ciru-tp2/bootstrap ai-exp/strix-ciru-tp2/runtime
    ai-exp/strix-ciru-tp2/cache code-gpt-main/node_modules
)
task_args=()
for task_path in "${task_excludes[@]}"; do task_args+=("--exclude=$task_path"); done
printf '%s\n' "${task_roots[@]}" > "$task_report/archive-$task_node-roots.txt"
printf '%s\n' "${task_excludes[@]}" > "$task_report/archive-$task_node-excludes.txt"
df -B1 /home/funboy > "$task_report/disk-$task_node-before.txt"
tar -C /home/funboy --use-compress-program='zstd -T2 -3' \
    "${task_args[@]}" -cf "$task_archive.partial" "${task_roots[@]}" \
    2> "$task_report/archive-$task_node.log"
zstd -t "$task_archive.partial" 2>> "$task_report/archive-$task_node.log"
tar -tf "$task_archive.partial" > "$task_report/archive-$task_node-files.txt"
mv -- "$task_archive.partial" "$task_archive"
sha256sum -- "$task_archive" > "$task_report/archive-$task_node.sha256"
printf 'ARCHIVE_VERIFIED %s\n' "$task_archive"
