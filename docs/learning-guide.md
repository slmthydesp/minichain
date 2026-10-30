# minichain 学习指南

minichain 是一条只存在内存里的转账链。每个进程都出块，也向对等方拉块。同一父块上可以分叉；节点按「高度更大，同高度则哈希更小」选出链头，换链头时把被放弃的交易退回交易池。两边对规范链算出同一个状态根。它用来建立「账户、签名、交易池、执行、出块、分叉、同步」这条链路，范围停在转账。

建议按下面的顺序读，每一层都先看测试再看实现。

| 顺序 | 文件 | 要回答的问题 |
| --- | --- | --- |
| 1 | `README.md`、`docs/superpowers/specs/2026-10-30-forks-design.md` | 两个进程怎样出块、分叉和回滚 |
| 2 | `chain/tx.go`、`chain/block.go`、`chain/address.go` | 交易和区块的字节从哪来 |
| 3 | `sig/sig.go` | 地址和发送方怎么从私钥推出来 |
| 4 | `chain/state.go` | 状态根为什么和插入顺序无关 |
| 5 | `chain/pool.go` | 什么交易能进池 |
| 6 | `chain/apply.go` | 一笔转账怎样改余额和 nonce |
| 7 | `chain/chain.go`、`chain/fork_test.go` | `Mine` 怎么挑交易，链头怎么选，回滚怎样退回交易 |
| 8 | `node/http.go`、`node/node.go`、`cmd/node/main.go`、`cmd/send/main.go` | 交易怎样转给对等方，分叉怎样沿父哈希拉回来 |

测试是表驱动的，直接调用 `Mine`、`Apply`、`FollowOnce`，不靠 `time.Sleep`。时间只出现在 `cmd/node` 的两个循环里。

## 架构

四个包，依赖方向是单向的：`cmd` 用 `node` 和 `chain`，`node` 用 `chain`，`chain` 用 `sig`。

```mermaid
flowchart TB
  subgraph clients [客户端]
    Send["cmd/send<br/>签名并 POST /tx"]
  end

  subgraph procB [进程 B 127.0.0.1:8546]
    HB["HTTP<br/>POST /tx · GET /status · GET /block"]
    MineB["每 1s Mine"]
    FollowB["每 200ms 沿父哈希拉块"]
    CB["Chain"]
    PB["Pool"]
    SB["State"]
  end

  subgraph procA [进程 A 127.0.0.1:8545]
    HA["HTTP<br/>POST /tx · GET /status · GET /block"]
    MineA["每 1s Mine"]
    FollowA["每 200ms 沿父哈希拉块"]
    CA["Chain"]
    PA["Pool"]
    SA["State"]
  end

  Send --> HA
  Send --> HB
  HB -->|"本地准入后转发同一 JSON"| HA
  HA -->|"本地准入后转发同一 JSON"| HB
  FollowB -->|"GET /block?hash="| HA
  FollowA -->|"GET /block?hash="| HB
  HB --> CB
  MineB --> CB
  FollowB --> CB
  CB --> PB
  CB --> SB
  HA --> CA
  MineA --> CA
  FollowA --> CA
  CA --> PA
  CA --> SA
```

A 和 B 各自 `chain.New()`，内存里都有同一份创世块：高度 0，父哈希 32 个 0，coinbase 是 20 个 0，只有演示账户。创世块两边都有，不需要从对等方拉。两个进程的 `--coinbase` 不同，空块的哈希才会不同。

演示账户写在 `sig.DemoKeyHex`：

- 私钥 `ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80`（Hardhat / Anvil 的第 0 个账户）
- 地址 `0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266`
- 创世余额 `1000000`，nonce `0`

`Chain` 用一把 `sync.Mutex` 罩住区块、状态和交易池。HTTP 和出块循环都通过 `AddTx`、`Mine`、`Append` 进出。

## 数据怎么变成哈希

整数都是大端。JSON 里的十六进制是小写，可以带 `0x`。

```text
签名原文 36 字节
┌────────────┬──────────┬──────────┐
│ to      20 │ value  8 │ nonce  8 │
└────────────┴──────────┴──────────┘

规范交易 101 字节 = 签名原文 + 签名
┌──────────────┬─────────┐
│ signing  36  │ sig  65 │
└──────────────┴─────────┘

区块哈希原文
┌────────────┬────────────┬──────────────┬────────────┬──────────────────┐
│ parent  32 │ root    32 │ coinbase  20 │ txCount  4 │ 每笔交易 101 字节 │
└────────────┴────────────┴──────────────┴────────────┴──────────────────┘

状态根：账户按地址字节升序，每个账户 36 字节再拼接，对整段做 Keccak-256
┌────────────┬───────────┬──────────┐
│ address 20 │ balance 8 │ nonce  8 │
└────────────┴───────────┴──────────┘
```

空账户列表的根是 Keccak-256(空字节)，`c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470`。这和以太坊里空字节串的 Keccak 是同一个常数。创世状态里有演示账户，所以创世状态根不是这个空根。

`Put` 插入或覆盖，`Delete` 删除。余额变成 0 不会自动删账户。同一批账户无论以什么顺序放入，根都相同，因为 `Root` 会先排序。

## 签名和地址

```mermaid
flowchart LR
  key["32 字节私钥"] --> pub["未压缩公钥 65 字节<br/>首字节 0x04"]
  pub --> body["去掉前缀的 64 字节"]
  body --> keccak["Keccak-256"]
  keccak --> addr["取最后 20 字节 = 地址"]

  txb["签名原文 36 字节"] --> h["Keccak-256"]
  h --> sig["SignCompact<br/>65 字节 header‖R‖S"]
  sig --> rec["RecoverCompact"]
  rec --> addr2["再走一遍地址算法 = 发送方"]
```

交易里不写发送方。`Tx.Sender` 对签名原文做 Keccak-256，再用 `ecdsa.RecoverCompact` 恢复公钥，然后取地址。`header` 是 27 或 28。签名长度不是 65，或恢复失败，都是 `ErrBadSignature`。

## 交易池和执行是两套规则

进池看的是「现在的账户」。执行看的是「按顺序应用到当前 nonce」。

| | 交易池 `Pool.Add` | 执行 `Apply` |
| --- | --- | --- |
| 签名 | 恢复失败 → `ErrBadSignature` | 同样 |
| nonce | 小于当前值 → `ErrNonceTooLow`；和池里同一发送方的某一笔相同 → `ErrNonceUsed`；大于当前值可以进池 | 必须等于当前 nonce，否则 `ErrNonceMismatch` |
| 余额 | `value` 大于账户当前余额 → `ErrInsufficientBalance`。不扣掉池里其他待处理交易 | `value` 大于发送方余额则失败 |
| 溢出 | 不检查 | 收款方余额加 `value` 超过 `uint64` → `ErrBalanceOverflow`，状态不动 |
| 成功后 | 原样放进池 | 发送方 nonce 加 1，余额减去金额；收款方不存在就建一个 nonce 为 0 的账户 |

账户不存在时，池和执行都把余额、nonce 当成 0。发送方和收款方是同一地址时，余额不变，nonce 仍加 1。

`Apply` 改的是调用方传入的状态。`Mine` 和 `Append` 都先 `Copy`，失败就丢掉副本。

`Pending` 的顺序是发送方地址升序，同一发送方再按 nonce 升序。这个顺序就是出块时的挑选顺序。

## 出块

每个进程都调用 `Mine(coinbase)`，只延长自己的规范链头。池空时仍然出一个空块，状态根不变。`cmd/node` 启动后先 `Mine` 一次，再按 `--interval`（默认 1 秒）循环，所以进程启动后很快就有高度 1。coinbase 写进区块哈希，两个进程的 coinbase 不同，空块才分得开。

```mermaid
flowchart TD
  start["Mine：锁住链"] --> scratch["复制头状态"]
  scratch --> pending["取出 Pending"]
  pending --> sender{"下一个发送方"}
  sender -->|"没有了"| seal["封块"]
  sender -->|"还有"| expect["期望 nonce = 该账户在副本上的当前 nonce"]
  expect --> check{"下一笔属于这个发送方，且 nonce 连续?"}
  check -->|"nonce 断档"| leave["停住这个发送方，后面的交易留在池里"]
  check -->|"是"| apply{"在副本上 Apply"}
  apply -->|"失败"| leave
  apply -->|"成功"| take["记入本块，期望 nonce 加 1"]
  take --> check
  leave --> sender
  seal --> block["新块：高度 = 头 + 1<br/>父哈希 = 头哈希<br/>coinbase = 本进程<br/>交易 = 选中的那些<br/>状态根 = 副本的根"]
  block --> choice["在全部已保存的块里选链头<br/>高度更大优先，同高度哈希更小优先"]
  choice --> commit["若链头变了：退回被放弃分支的交易<br/>删掉新规范链里的交易<br/>状态换成新链头的副本"]
```

挑选是按发送方一段一段看的。从该账户当前 nonce 起必须连续。碰到缺口，或者这笔 `Apply` 会失败，就停在这个发送方。更高的 nonce 留在池里，等以后某一块能接上。

这里有一个要亲自验证的行为：池的余额检查只和账户里已有的余额比。余额 `1000000` 时，nonce 0 和 nonce 1 可以各转 `1000000` 都进池。出块时 nonce 0 成功，nonce 1 在副本上余额不够，于是留在池里。下一块它还会失败，直到有一笔入账把余额补上。

## 跟块

有 `--peers` 时，每个进程每 `--poll`（默认 200ms）读对等方的 `GET /status`。本地已经有那个链头哈希就结束这一轮。否则 `GET /block?hash=` 从链头往父块取，直到父块本地已经有，再从最老的一块开始 `Append`。对等方连不上时打印 `sync:`，进程继续出块。一边退出后，另一边不退出。

`Append` 保存任何父块已经在本地的块：

1. `decodeBlock` 用同一套字节规则重算哈希，必须等于 JSON 里的 `hash`
2. 同一哈希再来一次：什么都不改，返回 nil
3. 父块必须已经保存。高度必须是父块高度加 1
4. 在父块状态的副本上按顺序 `Apply`，算出的根必须等于块里的 `stateRoot`

通过后保存块和状态副本，再选链头。新链头上的交易从池里删除。旧链头上、且新链没有花掉同一发送方和 nonce 的交易，按新状态重新进池。侧链上的块留在内存里，`BlocksAfter` 只列出规范链。

## 时序：交易发给 A

```mermaid
sequenceDiagram
  participant U as cmd/send
  participant A as 进程 A
  participant B as 进程 B

  U->>A: POST /tx
  A->>A: 恢复发送方，按链头状态准入
  A-->>U: 204，交易留在 A 的池
  A->>B: 转发同一 JSON。失败也不撤回 A 的池
  Note over A,B: 两边都可能把这笔打进自己的下一块
  B->>A: GET /status，再按父哈希 GET /block
  A-->>B: 缺的那些块
  B->>B: Append。更长的链成为链头，短链上的交易退回池
  Note over A,B: 规范链的 height 与 stateRoot 相同
```

## 时序：交易发给 B

B 用本机状态做准入，所以发之前要先等两边 `GET /status` 的 `height` 和 `stateRoot` 一致。

```mermaid
sequenceDiagram
  participant U as cmd/send
  participant B as 进程 B
  participant A as 进程 A

  U->>B: POST /tx
  B->>B: AddTx。本地拒绝则 400，不转发
  B->>A: POST 同一个 JSON
  A-->>B: 204、400 或连不上
  B-->>U: 本地已经收下，所以仍是 204
  Note over A,B: 转发失败不从 B 的池删除
  B->>A: 沿父哈希把 A 的规范链拉回来
  A->>B: 沿父哈希把 B 的规范链拉回来
  Note over A,B: 高度更大的那条成为两边的链头
```

出块和同步之间有一个窗口：一边的池已经没有这笔，另一边的池还有，两边状态根也不相同。再过最多一个轮询间隔，规范链对齐。

## HTTP

| 方法 | 路径 | 谁提供 | 作用 |
| --- | --- | --- | --- |
| `POST` | `/tx` | 每个进程 | 本机准入失败返回 `400`。本机成功返回 `204`，并尽力转发给每个对等方 |
| `GET` | `/status` | 每个进程 | 本机规范链头：`height`、`stateRoot`、`head` |
| `GET` | `/blocks?after=<height>` | 每个进程 | 规范链上高度大于 `after` 的块。不含创世块，也不含侧链 |
| `GET` | `/block?hash=<hex>` | 每个进程 | 按哈希取一块。没有则 `404` |

`cmd/send` 的 `--nonce` 必填，它不向节点查当前 nonce。`--key` 省略时用演示私钥。

```bash
go test ./...

# 终端 1
go run ./cmd/node --listen 127.0.0.1:8545 --peers http://127.0.0.1:8546 --coinbase 0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266

# 终端 2
go run ./cmd/node --listen 127.0.0.1:8546 --peers http://127.0.0.1:8545 --coinbase 0x0000000000000000000000000000000000000002

curl -s http://127.0.0.1:8545/status
curl -s http://127.0.0.1:8546/status

go run ./cmd/send \
  --url http://127.0.0.1:8545 \
  --to 0x0000000000000000000000000000000000000001 \
  --value 10 \
  --nonce 0
```

## 和以太坊概念的对应

读 go-ethereum 时可以这样对上：

- `Account.Nonce` 就是以太坊账户 nonce：每成功执行一笔，加 1，用来挡住重放。
- 交易池是一个简化的 txpool。这里允许未来 nonce 进池，但出块要求从当前 nonce 连续执行。
- `State.Root` 对应状态根的角色，算法是「排序后拼接再哈希」。以太坊用的是 Merkle Patricia Trie。
- `Block.Hash` 把父哈希、状态根、coinbase 和交易绑在一起。改其中任何一项，哈希都变，子块的父哈希就对不上。
- 多个进程出块对应短程分叉。这里用块数做链头选择，同高度比哈希。以太坊用投票权重，并另有最终性。没有节点发现，也没有落盘。链只在进程内存里，退出即丢失。
- 换链头时退回被放弃分支的交易，对应节点重组时把孤儿块里的交易放回交易池。
- 签名恢复发送方，和以太坊从 `v,r,s` 恢复 `from` 是同一件事。这里签的是 36 字节转账原文，用的是 `header || R || S`，没有 EIP-155 的 chain id。

这个仓库停在转账。EVM、合约、gas、点对点发现、数据库都不在代码里。下一步如果要往执行引擎走，就从 `Apply` 换成「按交易字节跑虚拟机，再用执行后的状态算根」。同步和出块的外壳可以保持不动。
