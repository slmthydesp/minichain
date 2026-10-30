# Chain id、gas、回执、按单价替换

日期：2026-10-30

在现有分叉链上补四件事。签名覆盖 chain id，默认并且只接受 `6666`。转账收取固定 gas，费用记入区块的 coinbase。区块承诺回执根。同一发送方、同一 nonce，gas 单价更高的交易替换池里的旧交易。仍然不落盘，不加 EVM。

## 不做

新的依赖、数据库、操作码、Merkle Patricia Trie、EIP-1559、按百分比抬价、失败交易入块。测试不调用 `time.Sleep`。`view` 不改；它忽略 JSON 里多出来的字段。

## 常量

```text
ChainID        6666
IntrinsicGas   21000
BlockGasLimit  100000
```

一笔成功的转账消耗 `IntrinsicGas`。`gasLimit` 小于它则拒绝。区块的 gas 上限是 `BlockGasLimit`。装进块时，剩余 gas 必须大于等于这笔的 `gasLimit`，装不下就停在这个发送方，后面的 nonce 留在池里，换下一个发送方。块上的 `gasUsed` 是实际消耗之和，不是 `gasLimit` 之和。

## 交易字节

签名原文 68 字节，大端：

```text
to         20
value      uint64
nonce      uint64
gasLimit   uint64
gasPrice   uint64
chainId    uint64
```

规范交易是签名原文加 65 字节签名，共 133 字节。`Sign` 的签名是 `func Sign(key []byte, to Address, value, nonce, gasLimit, gasPrice, chainID uint64) (Tx, error)`。

链只接受 `chainId == 6666`。签名覆盖 chain id：改这个字段后再恢复，签名失败。用别的 chain id 签出来的合法签名，恢复成功，但准入和执行返回 `wrong chain id`。

## 费用

池和执行都先检查，再改状态。溢出或失败时状态不动。

顺序：

1. 恢复发送方。失败为 `bad signature`。
2. `chainId` 不是 `6666`：`wrong chain id`。
3. `gasLimit < 21000`：`intrinsic gas`。
4. `gasLimit * gasPrice` 溢出 `uint64`：`balance overflow`。这个乘积是池里要预留的最高费用。
5. `value + gasLimit*gasPrice` 溢出：`balance overflow`。大于余额：`insufficient balance`。
6. nonce 规则见下。

执行成功时实际费用是 `21000 * gasPrice`，不是按 `gasLimit` 收。发送方余额减去 `value + 实际费用`，nonce 加 1。收款方不存在就建立，再加上 `value`。coinbase 不存在就建立，再加上实际费用。收款方或 coinbase 入账溢出为 `balance overflow`，状态不动。

发送方和收款方是同一地址时，`value` 不改变余额，实际费用仍然支付给 coinbase。发送方就是 coinbase 时，费用从自己扣下再加回。

`Apply` 改为 `func Apply(s *State, tx Tx) (gasUsed uint64, err error)`。成功返回 `21000`。

## 区块

`Block` 增加 `GasLimit`、`GasUsed`、`ReceiptsRoot`。`Mine` 写出的 `GasLimit` 总是 `100000`。空块的 `GasUsed` 为 0，回执根是 Keccak-256(空字节)，即 `c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470`。

区块哈希原文：

```text
parentHash     32
stateRoot      32
receiptsRoot   32
coinbase       20
gasLimit       uint64
gasUsed        uint64
txCount        uint32
txs            每笔 133 字节
```

## 回执

每笔入块交易一笔回执，顺序与交易相同。字段都是 `uint64`：`status`、`gasUsed`、`cumulativeGasUsed`。成功转账的 `status` 是 1，`gasUsed` 是 21000，`cumulativeGasUsed` 是本块到这笔为止的 gas 之和。

回执编码为 24 字节，三个 `uint64` 大端。回执根是按顺序拼接后的 Keccak-256。`Append` 重放时重算，与块上的根不一致就拒绝，链和状态都不变。

## 替换

同一发送方、同一 nonce 已在池中：

- 新交易的 `gasPrice` 更大：用新交易替换旧交易，返回 nil。
- 否则返回 `replacement underpriced`。

替换前仍要过签名、chain id、intrinsic gas、余额和 nonce 下限。单价相等不替换。

## JSON 与命令

交易 JSON 增加 `gasLimit`、`gasPrice`、`chainId`，都是十进制整数。区块 JSON 增加 `gasLimit`、`gasUsed`、`receiptsRoot`，以及 `receipts`。每笔回执是 `status`、`gasUsed`、`cumulativeGasUsed`。

`cmd/send` 增加 `--gas-limit`，默认 `21000`；`--gas-price`，默认 `1`。chain id 固定用 `6666`，没有开关。

## 测试

- 签名原文 68 字节，规范交易 133 字节。改 chain id、gasLimit 或 gasPrice，发送方恢复失败。
- chain id 为 6667 的合法签名被准入拒绝，错误文本是 `wrong chain id`。
- gasLimit 为 20999 被拒绝，错误文本是 `intrinsic gas`。
- 一笔转账后，发送方余额减少 `value + 21000*gasPrice`，coinbase 增加 `21000*gasPrice`，收款方增加 `value`。
- 区块 gas 上限装不下下一笔时，那笔留在池里，块的 `gasUsed` 只计已装入的。
- 空块回执根等于上面的空 Keccak。两笔交易的回执根随累计 gas 变化。改掉块上的回执根后 `Append` 拒绝。
- 同 nonce、更高 `gasPrice` 替换池中的交易。单价不更高则 `replacement underpriced`。
