# ecrecover：预编译、字节码和谁付钱

日期：2026-11-10

同一条 `hello world` 签名，分别用地址 `0x01` 和合约里的 secp256k1 点运算恢复地址，参考 https://my.feishu.cn/wiki/HRgPw7AgLi6JlPksOkRceh5Sngf#share-QEaUdlFZooLJw5xiUKJchIJtnVd 。两边都得到 Anvil 测试账户 `0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266`。下面的 gas 是函数体内的 `gasUsed`，不含选择器和打包返回值。

合约在 `contracts/EcrecoverGasCompare.sol`。签名由 `ecrecover.sh` 在本地用 `--no-hash` 签出。

| 参数 | 值 |
| --- | --- |
| 消息 | `hello world` |
| 哈希 | `0x47173285a8d7341e5e972fc677286384f802f8ef42a5ec5f03bbfa254cb01fad` |
| v | 28 |
| r | `0x3ee428fe71186c050ed93ada46fe27f4d252144592f434c2cec7e77deb9f12f4` |
| s | `0x2cc055a7f178b6c762dc121cef1e7fc6a611ed1c175285c730ffae9a76b33f75` |

## 同一条签名的 gasUsed

两组都是 `eth_call` 模拟，没有从账户扣 ETH。

| 实现 | Remix 实测 | solc 0.8.36，优化器 200 次 |
| --- | ---: | ---: |
| `0x01` 预编译 | 3,310 | 3,310 |
| 合约字节码 | 1,531,037 | 1,011,873 |
| 字节码 / 预编译 | 462 倍 | 306 倍 |

Remix 数字来自 `compare()` 的返回值。优化器对照是同一合约在本地 anvil 上的 `eth_call`。

## 3,310 里的 3,000

预编译按协议标价收费，不按它内部跑了多少本地指令收费。`ecrecover` 的标价是 3,000。其余约 310 是把 128 字节输入写入内存，再 `staticcall` 到 `0x01`。这段外围代码很短，开不开优化器都停在 3,310。

| 组成部分 | gas | 说明 |
| --- | ---: | --- |
| `0x01` 固定收费 | 3,000 | 签名合不合法都收满 |
| 准备输入和 `staticcall` | 310 | 128 字节：哈希、`v`、`r`、`s` |
| Remix 合计 | 3,310 | `recoverByPrecompile` 的 `gasUsed` |

## 字节码为什么到一百五十万

`recoverByBytecode` 不调用任何预编译。它在合约里做平方根、标量求逆，再对 secp256k1 的生成元和恢复出的点做 256 轮加倍和按位加法，最后 `keccak256` 出地址。每一轮都是一串 `MULMOD`。优化器把内部函数内联之后，同一条路径降到 1,011,873，仍是预编译的 306 倍。

OpenZeppelin v5.7.0 的 `ECDSA.recover` 没有自己算曲线。它拒绝 `s` 大于曲线阶一半的签名，然后调用 Solidity 的 `ecrecover`，也就是这次 `0x01`。钱包的 `personal_sign` 还要先包上 `\x19Ethereum Signed Message:\n`。这次实验用 `--no-hash` 签的是 `keccak256("hello world")` 本身，没有那层前缀。

## 模拟会计，入块才扣 ETH

节点执行时总会把 gas 数出来。数出来的结果要不要变成余额变动，取决于这次执行会不会写进区块。

| 入口 | 执行完留下什么 | 谁付钱 |
| --- | --- | --- |
| `eth_call`、`cast call`、Remix Call | 只返回数据。余额、nonce、存储都恢复原样 | 没有人付。gas 不够则这次模拟失败 |
| `eth_sendRawTransaction`、Remix Transact | 交易进池，打进区块后留下回执和状态 | 交易发送方付 `gasUsed × gasPrice`。恢复出的地址不付 |
| minichain `POST /tx` | 交易进池。出块时 `Apply` 才执行 | 入块才扣。没有只读执行接口 |

`ecrecover.sh` 用 `cast call` 打到 `0x01`。节点在本地跑完预编译就把地址返回来。测试私钥在 Sepolia 上没有余额也没关系，因为根本没有交易发送方。

### 一笔真交易里的 21000 和 3000

不带数据的转账先扣 21,000 固有 gas，然后才把剩余 gas 交给收款方。`gasLimit` 正好是 21,000 时，留给 `ecrecover` 的执行 gas 是 0，3,000 不够，金额退回，发送方仍为已用掉的 gas 付费。调用 `0x01` 还要为 128 字节输入另付数据 gas。

执行 gas 够且预编译成功时，附带的金额留在 `0x01`。签名不合法时 `ecrecover` 返回空并且调用成功，金额也不退。预编译不做 `CALL`，也不做 `SELFDESTRUCT`。`0x01` 到 `0x11` 以及 `0x100` 没有已知私钥，这笔余额转不出来。

## minichain 现在能做什么

预编译表只有 `0x01`，gas 同样是 3,000，输入布局和以太坊一致。`POST /tx` 只把交易放进池；`GET /account` 和 `GET /block` 只读已经入块的状态。待办里的「不改状态的执行」就是还没有的 `eth_call`：跑一遍字节码，回返回值，不增加 nonce，不改余额，也不产生回执。

和以太坊有一处不同：minichain 里带金额的 `CALLCODE` 会把金额转到 `0x01`。以太坊的 `CALLCODE` 不会。
