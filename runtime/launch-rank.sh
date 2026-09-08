#!/usr/bin/env bash
# External pinned CIRU engine only. Called by the Go all-rank controller.
set -Eeuo pipefail
task_engine=${1:?engine root}
task_rank=${2:?rank}
task_epoch=${3:?epoch}
task_port=${4:?HTTP port}
task_master=${5:?master port}
task_settings=${6:?isolated settings root}
[[ "$task_engine" == /* && "$task_settings" == /* ]] || exit 2
[[ "$task_rank" == 0 || "$task_rank" == 1 ]] || exit 2
[[ "$task_epoch" =~ ^[0-9]{10,16}$ && "$task_port" =~ ^[0-9]{4,5}$ && "$task_master" =~ ^[0-9]{4,5}$ ]] || exit 2
[[ "$(hostname)" == "0$((task_rank + 1))-EVO-X3" ]] || exit 2
export PATH="$task_engine/bootstrap/tools/bin:/usr/local/bin:/usr/bin:/bin"
unset LD_LIBRARY_PATH LD_PRELOAD PYTHONPATH PYTHONHOME VIRTUAL_ENV CMAKE_PREFIX_PATH
unset VLLM_PLUGINS CIRU_TRACE CIRU_TRACE_DIR CIRU_TRACE_TOKEN_COUNTS CIRU_TRACE_MAX_FORWARDS CIRU_TRACE_DEEP_MOE
unset CIRU_TRACE_DECODE_POSITION CIRU_TRACE_FOCUS_BATCH VLLM_NCCL_SO_PATH
unset ROCM_PATH ROCM_HOME ROCM_CORE_ROOT ROCM_DEVEL_ROOT ROCM_LIBRARIES_ROOT
unset HIP_PATH HIP_DEVICE_LIB_PATH CUDA_VISIBLE_DEVICES HIP_VISIBLE_DEVICES ROCR_VISIBLE_DEVICES CC CXX
export VLLM_SOURCE="$task_engine/runtime/vllm-glm53-strix"
export AITER_SOURCE="$task_engine/runtime/aiter-gfx1151"
export VLLM_VENV="$task_engine/venv"
export VLLM_RUNTIME_ENV="$VLLM_SOURCE/runtime-env.sh"
export CIRU_HOST_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu
export XDG_CACHE_HOME="$task_engine/cache" AITER_JIT_DIR="$task_engine/cache/aiter"
export TRITON_CACHE_DIR="$task_engine/cache/triton" TORCHINDUCTOR_CACHE_DIR="$task_engine/cache/torchinductor"
export GLM53_RUNTIME_SETTINGS_ROOT="$task_settings"
export HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 VLLM_NO_USAGE_STATS=1 DO_NOT_TRACK=1
export TRANSPORT_MODE=portable TRANSPORT_INTERFACE=thunderbolt0
export NCCL_SOCKET_IFNAME='=thunderbolt0' GLOO_SOCKET_IFNAME=thunderbolt0
export NCCL_NET=Socket NCCL_IB_DISABLE=1 NCCL_SOCKET_FAMILY=AF_INET
export NCCL_MIN_NCHANNELS=1 NCCL_MAX_NCHANNELS=1 NCCL_SOCKET_NTHREADS=1 NCCL_NSOCKS_PERTHREAD=1
export VLLM_NHI_ALLREDUCE=0 VLLM_NHI_M4_ALLREDUCE=0 VLLM_NHI_M8_ALLREDUCE=0 VLLM_NHI_PP768_ALLREDUCE=0 ALLOW_PRIVILEGED_NHI=0
source "$VLLM_RUNTIME_ENV"
export CIRU_SAFE_PREFILL=1 CIRU_CANONICAL_MOE=1
for task_override in dflash-tokens prefix-cache-enabled; do
    [[ ! -e "$GLM53_RUNTIME_SETTINGS_ROOT/$task_override" ]] || { echo "Unexpected settings override" >&2; exit 2; }
done
export MODEL_ROOT="$task_engine/artifacts/ciru" RUNTIME_ROOT="$task_engine/artifacts/ciru/runtime/gfx1151"
export DFLASH_MODEL="$task_engine/artifacts/dflash2" MASTER_ADDR=10.55.0.1 MASTER_PORT="$task_master"
export DFLASH_TOKENS=5 PREFIX_CACHE_ENABLED=0 CONTEXT_PROFILE=64k
export VLLM_IMPORT_FROM_WHEEL=1 VLLM_DFLASH_LOCAL_TP1=0 MAX_NUM_BATCHED_TOKENS=2304
export MAX_MODEL_LEN=65664 KV_CACHE_BYTES=6442450944 OMP_NUM_THREADS=1 PYTHONHASHSEED=1
exec bash "$MODEL_ROOT/launch-node.sh" "$task_rank" "10.55.0.$((task_rank + 1))" "$task_epoch" "$task_port"
