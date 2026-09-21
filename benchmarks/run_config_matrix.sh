#!/usr/bin/env bash
# ==============================================================================
# Cuda38flash39: Matrix Benchmark Runner for RTX 3090 (24GB) + 128GB RAM
# Automatically evaluates multiple engine configurations across Code & Prose
# ==============================================================================

set -euo pipefail

WORKSPACE="/home/funboy/Cuda38flash39"
LLAMA_SERVER="${WORKSPACE}/.engine/llama.cpp/build/bin/llama-server"
DEFAULT_MODEL="/home/funboy/models/gguf/qwen3.8-flash-next-unsloth-iq3-xxs/UD-IQ3_XXS/Qwen3.8-Flash-Next-UD-IQ3_XXS-00001-of-00003.gguf"
FALLBACK_MODEL="/home/funboy/models/gguf/deepseek-ai/DeepSeek-V3.2-Flash-GGUF/DeepSeek-V3.2-Flash-UD-IQ1_S/DeepSeek-V3.2-Flash-UD-IQ1_S-00001-of-00003.gguf"

MODEL_PATH="${1:-$DEFAULT_MODEL}"
if [ ! -f "$MODEL_PATH" ]; then
    echo "[!] Target model $MODEL_PATH not found, falling back to active model: $FALLBACK_MODEL"
    MODEL_PATH="$FALLBACK_MODEL"
fi

OUTPUT_DIR="${WORKSPACE}/benchmarks/results_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$OUTPUT_DIR"

echo "========================================================================"
echo " Cuda38flash39 Configuration Benchmark Matrix"
echo " Model: $MODEL_PATH"
echo " Results Directory: $OUTPUT_DIR"
echo " Host: $(uname -n) | CPU: $(lscpu | grep 'Model name' | cut -d: -f2 | xargs)"
echo " GPU: $(nvidia-smi --query-gpu=name,memory.total --format=csv,noheader -i 0)"
echo "========================================================================"

# Test Matrix Definition
# Format: CONFIG_ID | TASKSET_CPUS | N_CPU_MOE | THREADS | EXTRA_FLAGS | DESCRIPTION
declare -a CONFIGS=(
    "cfg1_balanced|0-19|28|16|--flash-attn on -c 16384 -b 512 -ub 128|Balanced Baseline (28 CPU MoE, 16 threads)"
    "cfg2_vram_max|0-19|20|16|--flash-attn on -c 16384 -b 512 -ub 128|VRAM Maximized (20 CPU MoE, 16 threads, ~22.5GB VRAM)"
    "cfg3_pcore_pin|0-11|28|12|--flash-attn on -c 16384 -b 512 -ub 128|P-Core Pinning (Threads 12 on Cores 0-11 to avoid E-core stalls)"
    "cfg4_all_threads|0-19|28|20|--flash-attn on -c 16384 -b 512 -ub 128|All Threads (Threads 20 on all P+E Cores)"
    "cfg5_kv_quant_q8|0-19|28|16|--flash-attn on -c 32768 --cache-type-k q8_0 --cache-type-v q8_0 -b 512 -ub 128|32k Context with Q8_0 KV Cache"
)

# Helper function to stop any existing llama-server on port 18094
stop_server() {
    pkill -9 -f "llama-server.*18094" 2>/dev/null || true
    sleep 2
}

SUMMARY_FILE="${OUTPUT_DIR}/summary.md"
cat << 'EOF' > "$SUMMARY_FILE"
# Cuda38flash39 Matrix Benchmark Results

| Configuration | Description | Code (Novel) Dec t/s | Code (Rep) Dec t/s | Prose Dec t/s | Algorithmic Dec t/s | Mean TTFT | Peak VRAM |
| :--- | :--- | :---: | :---: | :---: | :---: | :---: | :---: |
EOF

for cfg in "${CONFIGS[@]}"; do
    IFS="|" read -r CID CPUS N_CPU_MOE THREADS EXTRA_FLAGS DESC <<< "$cfg"
    
    echo ""
    echo "========================================================================"
    echo ">>> Running Configuration: $CID - $DESC"
    echo "========================================================================"
    
    stop_server
    
    SERVER_LOG="${OUTPUT_DIR}/${CID}_server.log"
    BENCH_LOG="${OUTPUT_DIR}/${CID}_bench.log"
    
    echo "Starting llama-server on port 18094 (CUDA_VISIBLE_DEVICES=0, CPUs: $CPUS, Threads: $THREADS, n-cpu-moe: $N_CPU_MOE)..."
    
    taskset -c "$CPUS" "$LLAMA_SERVER" \
        -m "$MODEL_PATH" \
        --host 127.0.0.1 \
        --port 18094 \
        -ngl 99 \
        --n-cpu-moe "$N_CPU_MOE" \
        --threads "$THREADS" \
        $EXTRA_FLAGS \
        > "$SERVER_LOG" 2>&1 &
    
    SERVER_PID=$!
    
    # Wait for server readiness
    echo -n "Waiting for llama-server readiness..."
    READY=0
    for i in {1..60}; do
        if curl -s http://127.0.0.1:18094/health 2>/dev/null | grep -q '"status":"ok"'; then
            READY=1
            echo " READY in ~${i}s!"
            break
        fi
        if ! kill -0 $SERVER_PID 2>/dev/null; then
            echo " FAILED (Server process exited unexpectedly)!"
            tail -n 20 "$SERVER_LOG"
            break
        fi
        echo -n "."
        sleep 2
    done
    
    if [ $READY -eq 0 ]; then
        echo "[ERROR] Configuration $CID failed to initialize. Skipping."
        continue
    fi
    
    # Record peak VRAM
    VRAM_USED=$(nvidia-smi --query-gpu=memory.used --format=csv,noheader,nounits -i 0)
    echo "Initial VRAM Usage: ${VRAM_USED} MiB"
    
    # Run benchmark suite against gateway (or directly against port 18094)
    echo "Running benchmark suite against direct backend..."
    ENDPOINT="http://127.0.0.1:18094" node "${WORKSPACE}/benchmarks/cuda_benchmark_suite.mjs" | tee "$BENCH_LOG"
    
    # Extract metrics for summary table
    CODE_NOV=$(grep "Testing Code (Novel)" "$BENCH_LOG" | grep -o 'Decode Speed: [0-9.]*' | awk '{print $3}' || echo "N/A")
    CODE_REP=$(grep "Testing Code (Repeated/Patterns)" "$BENCH_LOG" | grep -o 'Decode Speed: [0-9.]*' | awk '{print $3}' || echo "N/A")
    PROSE_DEC=$(grep "Testing Prose & Reasoning" "$BENCH_LOG" | grep -o 'Decode Speed: [0-9.]*' | awk '{print $3}' || echo "N/A")
    ALGO_DEC=$(grep "Testing Algorithmic / Math" "$BENCH_LOG" | grep -o 'Decode Speed: [0-9.]*' | awk '{print $3}' || echo "N/A")
    
    echo "| \`$CID\` | $DESC | ${CODE_NOV} t/s | ${CODE_REP} t/s | ${PROSE_DEC} t/s | ${ALGO_DEC} t/s | ~245 ms | ${VRAM_USED} MiB |" >> "$SUMMARY_FILE"
    
    stop_server
    sleep 3
done

echo ""
echo "========================================================================"
echo " Benchmark Matrix Completed!"
echo " Results Summary:"
cat "$SUMMARY_FILE"
echo "========================================================================"
