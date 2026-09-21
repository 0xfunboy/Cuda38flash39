#!/usr/bin/env bash
# CUDA inference engine launcher for Cuda38flash39
# Targets single NVIDIA GeForce RTX 3090 (24GB VRAM) on PCIe Gen4 x16 (GPU 0)
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Enforce GPU 0 (PCIe Gen4 x16 slot, 0000:01:00.0)
export CUDA_VISIBLE_DEVICES=0

# Verify GPU 0 PCIe link and name
echo "==> Verifying CUDA target device (RTX 3090 on PCIe x16)..."
nvidia-smi --id=0 --query-gpu=name,pci.bus_id,pcie.link.gen.current,pcie.link.width.current --format=csv,noheader

MODEL_DIR="${MODEL_DIR:-/home/funboy/models/gguf/qwen3.8-flash-next-unsloth-iq3-xxs/UD-IQ3_XXS}"
MODEL_PATH="${1:-$MODEL_DIR/Qwen3.8-Flash-Next-UD-IQ3_XXS-00001-of-00003.gguf}"
LLAMA_SERVER="${LLAMA_SERVER:-$ROOT_DIR/.engine/llama.cpp/build/bin/llama-server}"
PORT="${PORT:-18094}"
HOST="${HOST:-127.0.0.1}"
CTX_SIZE="${CTX_SIZE:-8192}"
BATCH_SIZE="${BATCH_SIZE:-2048}"
UBATCH_SIZE="${UBATCH_SIZE:-512}"
THREADS="${THREADS:-12}"
CPUS="${CPUS:-0-11}"
N_CPU_MOE="${N_CPU_MOE:-30}"

if [[ ! -x "$LLAMA_SERVER" ]]; then
    echo "Error: llama-server executable not found at $LLAMA_SERVER" >&2
    echo "Build the CUDA engine first: cmake --build $ROOT_DIR/.engine/llama.cpp/build -j 16" >&2
    exit 1
fi

if [[ ! -f "$MODEL_PATH" ]]; then
    echo "Warning: Model file not found at $MODEL_PATH" >&2
    echo "Download Qwen 3.8 Flash Next using: go run tools/download_qwen.go" >&2
fi

echo "==> Starting CUDA inference engine on $HOST:$PORT..."
echo "    Model: $MODEL_PATH"
echo "    CPU Affinity: taskset -c $CPUS (P-cores optimized, avoiding E-core barrier stalls)"
echo "    VRAM Allocation: -ngl 99 --n-cpu-moe $N_CPU_MOE (23.2GB VRAM on GPU 0, PCIe Gen4 x16)"
echo "    RAM Allocation: --load-mode none (100% direct physical DDR RAM, zero mmap/NVMe paging)"
echo "    Context: $CTX_SIZE | Threads: $THREADS | Batch: $BATCH_SIZE | UBatch: $UBATCH_SIZE"

exec taskset -c "$CPUS" "$LLAMA_SERVER" \
    --model "$MODEL_PATH" \
    --host "$HOST" \
    --port "$PORT" \
    -ngl 99 \
    --n-cpu-moe "$N_CPU_MOE" \
    --load-mode none \
    --ctx-size "$CTX_SIZE" \
    --batch-size "$BATCH_SIZE" \
    --ubatch-size "$UBATCH_SIZE" \
    --threads "$THREADS" \
    --flash-attn on \
    --metrics
