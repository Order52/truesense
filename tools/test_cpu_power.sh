#!/usr/bin/env bash
# CPU/GPU power tests that need root. Every write is read back and restored. The undervolt is READ ONLY here (no offset is ever written).
#   sudo tools/test_cpu_power.sh
set -u
[ "$(id -u)" -eq 0 ] || { echo "run with sudo"; exit 1; }
ROOT=$(realpath "$(dirname "$0")/.."); OUT="$ROOT/docs/hardware-data/cpu_power_test.txt"
RPS() { PYTHONPATH="$ROOT/src" python3 -I -c "import sys; sys.path.insert(0,'$ROOT/src'); from rpredatorsense.cli import main; main()" "$@" 2>&1; }
{
echo "== $(date +%T) RAPL baseline"; RPS cpu info | head -8
echo "== RAPL write test: PL1=70 W PL2=80 W (the running daemon re-asserts 80/80 within ~3 s anyway)"
RPS cpu limits 70 80; echo "-- restore 80/80"; RPS cpu limits 80 80
echo "== range check (must refuse 200 W)"; RPS cpu limits 200 200
echo "== NVIDIA power limit"
nvidia-smi --query-gpu=power.limit,power.default_limit,power.min_limit,power.max_limit --format=csv
echo "-- try setting the default limit (80 W) back to itself:"; nvidia-smi -pl 80 2>&1 | head -3
echo "== MSR / undervolt (READ ONLY)"; modprobe msr 2>&1
echo "-- core plane offset:";  RPS cpu uv-get --plane 0
echo "-- cache plane offset:"; RPS cpu uv-get --plane 2
echo "-- MSR 0x610 lock bit / FLEX_RATIO:"; RPS cpu info | grep msr_lock; rdmsr -p0 0x194 2>&1
} 2>&1 | tee "$OUT"
chown "${SUDO_USER:-root}": "$OUT"; echo "saved $OUT"
