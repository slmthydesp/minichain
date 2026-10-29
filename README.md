# minichain

两个进程跑一条内存里的转账链。A 出块。A 和 B 都接受已签名转账。B 向 A 拉块，两边对同一批转账得到同一个状态根。

没有 EVM、合约、gas、节点发现和数据库。

## 测试

```bash
go test ./...
```

## 启动

终端 1，出块进程 A，监听 `127.0.0.1:8545`，每秒出一个块：

```bash
go run ./cmd/node
```

终端 2，跟随进程 B，监听 `127.0.0.1:8546`：

```bash
go run ./cmd/node --peer http://127.0.0.1:8545
```

先等两边高度一致，再发交易。B 用本机状态检查 nonce 和余额。

```bash
curl -s http://127.0.0.1:8545/status
curl -s http://127.0.0.1:8546/status
```

两边的 `height` 和 `stateRoot` 相同之后再继续。

## 发一笔转账

演示私钥是 `ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80`，地址是 `0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266`，创世余额 `1000000`。`--key` 不写就用这把钥匙。

发给 A：

```bash
go run ./cmd/send \
  --url http://127.0.0.1:8545 \
  --to 0x0000000000000000000000000000000000000001 \
  --value 10 \
  --nonce 0
```

发给 B。B 会检查自己的状态，再把同一笔转发给 A：

```bash
go run ./cmd/send \
  --url http://127.0.0.1:8546 \
  --to 0x0000000000000000000000000000000000000001 \
  --value 5 \
  --nonce 1
```

等 A 的下一个出块间隔。然后再看两个 status。两边的 `stateRoot` 相同，`height` 相同。

## A 关掉的时候

在终端 1 按 Ctrl-C。B 的下一次拉块失败，标准错误打印 `sync:`，进程以状态码 1 退出。
