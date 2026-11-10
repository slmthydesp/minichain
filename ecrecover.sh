#!/bin/bash

RPC=https://ethereum-sepolia-rpc.publicnode.com
KEY=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
HASH=$(cast keccak ecrecover)
SIG=$(cast wallet sign --private-key $KEY --no-hash $HASH)

# 65 字节签名拆成 r、s、v。v 是 1c，也就是 28
R=0x${SIG:2:64}
S=0x${SIG:66:64}
V=0x${SIG:130:2}

# 输入是 128 字节：哈希、v、r、s，各 32 字节
INPUT=$(cast abi-encode "x(bytes32,uint256,bytes32,bytes32)" $HASH $V $R $S)

cast call 0x0000000000000000000000000000000000000001 \
  --data $INPUT --rpc-url $RPC