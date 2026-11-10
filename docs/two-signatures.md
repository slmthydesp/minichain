# 同一把私钥和同一个哈希有两个签名

日期：2026-11-10

secp256k1 上，一把私钥对同一个 32 字节摘要可以对应两个都合法的签名。`r` 相同，`s` 换成曲线阶减 `s`，恢复标记在 27 和 28 之间对调。两份签名的字节不同，`ecrecover` 恢复出的是同一个地址。

`chain/precompile_test.go` 的 `TestEcrecoverAcceptsCompactSignature` 测的就是这件事。下面的程序用演示私钥把过程跑一遍。

## 签名长什么样

`sig.Sign` 返回 65 字节，顺序是恢复标记、`r`、`s`：

```text
┌──────────┬──────────┬──────────┐
│ v      1 │ r     32 │ s     32 │
└──────────┴──────────┴──────────┘
```

`v` 是 `27` 加上 `0` 到 `3`。常见的是 27 或 28，差在公钥 Y 坐标的奇偶。29 和 30 是同一种奇偶，只是 `r` 所在的 X 坐标多加了一次曲线阶。

曲线阶 `N` 是

```text
FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141
```

`(r, s)` 和 `(r, N-s)` 都落在 `1` 到 `N-1` 里，验签都通过。换成 `N-s` 之后，恢复公钥时要走另一侧的 Y，所以 `v` 的最低位也要翻过来：27 换成 28，28 换成 27。

翻这一位时先减去 27，再对剩下的数异或 1，最后加回 27：

```text
27 + ((v - 27) ^ 1)
```

这样 29 和 30 也会成对互换。直接把整个字节异或 1，27 会变成 26，26 不是合法的恢复标记。

## 程序

在本模块目录里把下面的代码存成一个 `.go` 文件再 `go run`。依赖就是 `go.mod` 里的 secp256k1 和 `golang.org/x/crypto`。

```go
package main

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

func main() {
	key, _ := hex.DecodeString("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	hash := keccak([]byte("ecrecover"))

	sig := ecdsa.SignCompact(secp256k1.PrivKeyFromBytes(key), hash[:], false)
	alt := other(sig)

	fmt.Printf("hash %x\n", hash)
	fmt.Printf("sig  v=%d r=%x s=%x\n", sig[0], sig[1:33], sig[33:])
	fmt.Printf("alt  v=%d r=%x s=%x\n", alt[0], alt[1:33], alt[33:])

	fmt.Printf("addr %x\n", address(hash[:], sig))
	fmt.Printf("alt  %x\n", address(hash[:], alt))
}

// other 造出另一份合法签名：s 换成 N-s，v 的奇偶对调，r 不动。
func other(sig []byte) []byte {
	n, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16)
	s := new(big.Int).SetBytes(sig[33:])
	s.Sub(n, s)

	out := append([]byte(nil), sig...)
	out[0] = 27 + ((sig[0] - 27) ^ 1)
	copy(out[33:], s.FillBytes(make([]byte, 32)))
	return out
}

func address(hash, sig []byte) [20]byte {
	pub, _, err := ecdsa.RecoverCompact(sig, hash)
	if err != nil {
		panic(err)
	}
	raw := pub.SerializeUncompressed()
	sum := keccak(raw[1:])
	var addr [20]byte
	copy(addr[:], sum[12:])
	return addr
}

func keccak(data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(data)
	sum := h.Sum(nil)
	var out [32]byte
	copy(out[:], sum)
	return out
}
```

演示私钥对应的地址是 `0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266`。对 `"ecrecover"` 这 9 个字节做 Keccak-256，再签名，一次运行得到：

```text
hash 8c3ce18b987029f7e3517f7e6a2a2690371489ed6f64d9f011137699ac809766
sig  v=28 r=98019ebbac998cde988d25950034f630a2defee2d255a617e0441c47483a0a08 s=23725b2f784595820b7f03023cb320da6952aba2ece86dead8e82036757a14cc
alt  v=27 r=98019ebbac998cde988d25950034f630a2defee2d255a617e0441c47483a0a08 s=dc8da4d087ba6a7df480fcfdc34cdf24515c3143c2603250e6ea3e565abc2c75
addr f39fd6e51aad88f6f4ce6ab8827279cfffb92266
alt  f39fd6e51aad88f6f4ce6ab8827279cfffb92266
```

`r` 相同，`v` 从 28 变成 27，`s` 变成 `N-s`。两个地址都是演示账户。

只改 `s`、留下原来的 `v`，恢复仍然能算出一个地址，但那是 `0x742ca204da0b892b3b786cf5af970b054ed8209a`，已经不是这把私钥。

## 和交易签名的差别

以太坊从 Homestead（EIP-2）起，放进交易里的 `s` 必须小于等于 `N/2`。两条里只留 `s` 较小的那条，避免同一笔交易换一份签名就得到另一个交易哈希。

地址 `0x01` 的预编译不套这条限制。`s` 和 `N-s` 只要都在 `1` 到 `N-1`，调用都成功，返回同一个地址。minichain 的 `ecrecover` 和 `sig.Recover` 也是这样。
