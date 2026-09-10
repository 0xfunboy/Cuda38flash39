#!/usr/bin/env bash
# Exact, disposable build/research residue. Keep operational JIT caches and weights.
set -Eeuo pipefail
umask 077
task_product=/home/funboy/StrixHaloClusterGLM
case "$(hostname)" in 01-EVO-X3) task_node=node01;; 02-EVO-X3) task_node=node02;; *) exit 2;; esac
task_report="$task_product/reports/SYSTEM-CLEANUP-001"
jq -e '.status == "PASS"' "$task_report/prune-$task_node.json" >/dev/null
task_log="$task_report/residue-$task_node.txt"
[[ ! -e "$task_log" ]] || exit 2
task_paths=(
    "$task_product/.engine/scripts"
    "$task_product/.engine/config"
    "$task_product/.engine/examples"
    "$task_product/.engine/patches"
    "$task_product/.engine/vendor-source"
    "$task_product/.engine/vendor"
    "$task_product/.engine/.git"
    "$task_product/.engine/cache/uv"
    "$task_product/.engine/cache/tmp"
    "$task_product/.engine/state/m6opt002"
    /home/funboy/.cache/go-build
    /home/funboy/.cache/ccache
    /home/funboy/.cache/pip
    /home/funboy/.npm/_cacache
    /home/funboy/.config/llama-strix
    /home/funboy/.local/state/llama-strix
)
for task_path in "${task_paths[@]}"; do
    [[ -e "$task_path" ]] || continue
    [[ -d "$task_path" && ! -L "$task_path" && "$(realpath "$task_path")" == "$task_path" ]] || exit 2
    du -sh -- "$task_path" >> "$task_log"
    rm -rf --one-file-system -- "$task_path"
done
# Completed download staging aliases: unlink ONLY if the canonical checkpoint
# exists and is the SAME inode. This does not remove model data or partial work.
while IFS= read -r -d '' task_part; do
    task_final=${task_part%.part}
    if [[ -f "$task_final" && "$task_part" -ef "$task_final" ]]; then
        printf 'same-inode completed staging alias: %s\n' "$task_part" >> "$task_log"
        unlink -- "$task_part"
    fi
done < <(find /home/funboy/models/ciru-glm53-flash -type f -name '*.safetensors.part' -print0)
printf 'RESIDUE_REMOVED %s; runtime venv/source/native files/JIT caches retained\n' "$task_node"
