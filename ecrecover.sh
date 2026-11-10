#!/bin/bash
set -euo pipefail

RPC=https://ethereum-sepolia.publicnode.com
KEY=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
MSG="hello world"
HASH=$(cast keccak "$MSG")
SIG=$(cast wallet sign --private-key "$KEY" --no-hash "$HASH")

# 65 字节签名拆成 r、s、v。v 是 27（0x1b）或 28（0x1c）。
R=0x${SIG:2:64}
S=0x${SIG:66:64}
V=0x${SIG:130:2}

echo "MSG: $MSG"
echo "HASH: $HASH"
echo "SIG: $SIG"
echo "R: $R"
echo "S: $S"
echo "V: $V"

# 输入是 128 字节：哈希、v、r、s，各 32 字节
INPUT=$(cast abi-encode "x(bytes32,uint256,bytes32,bytes32)" "$HASH" "$V" "$R" "$S")
echo "INPUT: $INPUT"

GOT=$(cast call 0x0000000000000000000000000000000000000001 \
  --data "$INPUT" --rpc-url "$RPC")
echo "GOT: $GOT"

ADDR=$(cast to-check-sum-address "$(cast parse-bytes32-address "$GOT")")
echo "ADDR: $ADDR"

WANT=$(cast wallet address --private-key "$KEY")
echo "WANT: $WANT"

if [ "$ADDR" != "$WANT" ]; then
  echo "recovered $ADDR, want $WANT" >&2
  exit 1
fi
echo "$ADDR"