# 模拟执行和打包执行

日期：2026-11-10

节点执行时总会数 gas。数出来的结果要不要变成余额变动，取决于这次执行会不会写进区块。`eth_call` 只模拟。`eth_sendRawTransaction` 不跑合约，只做准入检查，把交易放进池。合约字节码是出块组装区块时才跑的。

## 模拟时 gas 不够会报错

会。`eth_call`、`cast call`、Remix 里的 Call 不扣账户余额，但 gas 计数器照常走。计数器到 0 时这次调用失败，RPC 返回错误，状态改动丢掉。

go-ethereum 的 `eth_call` 在 `internal/ethapi/api.go` 的 `Call` 里：执行结果带错误就原样返回。`ErrExecutionReverted` 会带上 revert 数据。执行 gas 耗尽同样是错误，调用方拿到的是失败，不是一个成功的返回值。

这里有两种「不够」，不要混在一起。

| 说的是什么 | `eth_call` 怎么处理 |
| --- | --- |
| 这次调用给出的 gas 上限盖不住执行 | 报错。常见是 out of gas，或者 gas 低于 21,000 再加数据 gas、再加 `ecrecover` 的 3,000 |
| 账户里的 ETH 不够付 `gasUsed × gasPrice` | 不查这笔费用。模拟没有发送方要付的账单 |

模拟仍会查和这次调用直接相关的状态。`from` 的余额不够支付调用附带的 `value` 时，转账失败，这次模拟报错，链上余额不动。`from` 可以省略，这时调用方按零地址算，不能附带金额。

没在请求里写 gas 时，节点用自己的 RPC gas 上限，通常不低于区块 gas 上限。所以上面 Remix 里一百五十万 gas 的字节码恢复能跑完。把同一笔调用的 gas 改成 21,000，模拟会因为不够 3,000 而失败，账户余额不变。

`eth_estimateGas` 也是在副本上反复模拟，找出能成功的最小 gas。它不是扣费，也不是把交易放进池。

## `eth_sendRawTransaction` 的检查顺序

这条 RPC 不提前把合约跑一遍。钱包或脚本如果想事先知道会不会 revert，要自己再调 `eth_call` 或 `eth_estimateGas`。节点收下交易时不做这件事。

`SendRawTransaction` 先解出已签名的交易，再交给 `SubmitTransaction`，最后是 `SendTx` 进交易池。池的检查分成两段，都在 `core/txpool/validation.go`。

不读账户状态的一段是 `ValidateTransaction`：

1. 交易类型、大小、当前分叉是否允许这种类型。
2. `gas` 不超过区块 gas 上限，费用字段没有溢出，`maxFeePerGas` 不低于小费。
3. 用签名恢复发送方。签名不对，这里就拒绝。
4. `gas` 不低于固有 gas。Prague 之后还要盖住 calldata 的 floor gas。固有 gas 只按交易字节算，不跑合约。
5. 小费不低于池子要求的最低小费。blob 交易另查 sidecar。

读账户状态的一段是 `ValidateTransactionWithState`：

1. nonce 不得低于账户当前 nonce。有的池还拒绝把 nonce 空号留在中间。
2. 余额不得低于这笔交易的最高占用：`value + gas × gasFeeCap`，再算上池里这个账户已经占住的费用。
3. 这是按 gas 上限预留，不是按执行后的 `gasUsed` 扣款。合约字节码仍然没有跑。

两段都过了，交易进池等待。RPC 返回交易哈希。返回哈希只说明池收下了，不说明合约已经执行，也不说明它一定会被打进下一个块。

## 打包的时候才执行

出块程序从池里挑交易，在链头状态的副本上逐笔执行，执行完才算出状态根、回执根，然后密封区块。顺序是：选交易，执行，把交易和回执写进正在组装的块，最后广播这块。不是先广播一块空壳再补执行，也不是交易一进池就执行。

go-ethereum 的矿工在 `miner/worker.go` 里对选中的交易调用 `ApplyTransaction`。这一步才进入 EVM。

执行结果分三种：

| 执行结果 | 会不会进块 | 余额 |
| --- | --- | --- |
| 成功 | 进块，回执状态为 1 | 扣 `gasUsed` 对应的费用，`value` 转到收款方 |
| revert，或执行 gas 在块里耗尽 | 进块，回执状态为 0 | `value` 退回，已烧掉的 gas 费仍扣 |
| nonce 对不上、余额不够、或别的共识错误 | 不进块。打包程序跳过它 | 链上不动。它留在池里或被丢掉 |

别的节点收到块之后会把块里的交易再执行一遍，核对状态根。核对失败则这块无效。所以执行发生两次：出块时一次，验证时一次。两次都在写块或验块的过程里，不是在 `eth_sendRawTransaction` 返回的那一刻。

## minichain 对应哪一段

minichain 没有 `eth_call`。`POST /tx` 走 `Chain.AddTx`，和上面的池检查同类：

1. 从签名恢复发送方。签不对就拒绝。
2. `checkTxAdmission` 核对 chain id、固有 gas，以及余额是否盖住 `value + gasLimit × gasPrice`。
3. nonce 不得低于账户当前 nonce。同一 nonce 只有 gas 单价更高才能替换。
4. 从账户 nonce 起连续占住的交易，一起占用余额。

这里不调用 `Apply`。交易先待在池里。

`Chain.Mine` 出块时才执行。它复制链头状态，按 gas 单价挑选，在副本上对每一笔调用 `Apply`。`Apply` 返回错误就关掉这个发送方，本块里已经装上的保留。回退和停机仍有回执，照样入块：nonce 增加，费用按规则扣，失败的 `value` 退回。装完才计算三个树根，把块挂到链头，并从池里删掉已经入块的交易。

待办里的「不改状态的执行」就是还没有的模拟接口：同一段字节码跑一遍，回返回值，不增加 nonce，不改余额，也不产生回执。
