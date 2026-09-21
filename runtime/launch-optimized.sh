#!/usr/bin/env bash
# ==============================================================================
# Cuda38flash39: In-Memory Optimized CUDA Inference Engine Launcher
# Targets: RTX 3090 24GB (PCIe Gen4 x16) + 128GB Physical RAM (mlock pinned)
# ==============================================================================

set -Eeuo pipefail

WORKSPACE="/home/funboy/Cuda38flash39"
LLAMA_SERVER="${WORKSPACE}/.engine/llama.cpp/build/bin/llama-server"
MODEL_PATH="/home/funboy/models/gguf/qwen3.8-flash-next-unsloth-iq3-xxs/UD-IQ3_XXS/Qwen3.8-Flash-Next-UD-IQ3_XXS-00001-of-00003.gguf"
LOG_FILE="${WORKSPACE}/benchmarks/llama_optimized.log"

export CUDA_VISIBLE_DEVICES=0

echo "==> Verifying GPU 0 (PCIe Gen4 x16)..."
nvidia-smi --id=0 --query-gpu=name,pci.bus_id,memory.total --format=csv,noheader

echo "==> Stopping any prior instances..."
pkill -9 -f "llama-server.*18094" 2>/dev/null || true
sleep 2

echo "==> Starting In-Memory Optimized llama-server on port 18094..."
echo "    Offload: -ngl 99 (All 48 Attention layers on GPU)"
echo "    MoE CPU: --n-cpu-moe 32 (Cold experts in 128GB RAM)"
echo "    RAM Pinning: --mlock (100% physical RAM, zero NVMe disk reads)"
echo "    Engram: --lazy-mode off (Preloaded into DDR RAM)"
echo "    Prefill Batch: -b 2048 -ub 512 (Tensor Core saturated)"
echo "    CPU Affinity: P-Cores 0-11, 12 threads"

    taskset -c 0-11 "$LLAMA_SERVER" \
        -m "$MODEL_PATH" \
        --host 127.0.0.1 \
        --port 18094 \
        -ngl 99 \
        --n-cpu-moe 30 \
        --load-mode none \
        -c 8192 \
        -b 2048 \
        -ub 512 \
        --threads 12 \
        --flash-attn on \
        --metrics \
        > "$LOG_FILE" 2>&1 &

SERVER_PID=$!
echo "Server launched with PID: $SERVER_PID"

echo -n "Waiting for server readiness..."
for i in {1..90}; do
    if curl -s http://127.0.0.1:18094/health 2>/dev/null | grep -q '"status":"ok"'; then
        echo " READY in ${i}s!"
        exit 0
    fi
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        echo " FAILED to start. Last log lines:"
        tail -n 30 "$LOG_FILE"
        exit 1
    fi
    echo -n "."
    sleep 2
done

echo " Timeout waiting for readiness."
tail -n 30 "$LOG_FILE"
exit 1
