# Minichain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 做一个内存里的转账链：进程 A 出块，A 和 B 都接受用户交易，B 把块从 A 拉过去，两边算出同一个状态根。

**Architecture:** `sig` 只做 Keccak-256 和 secp256k1，不引用 `chain`。`chain` 持有账户、交易、区块、状态、交易池和链，并引用 `sig`。`node` 用 HTTP 把交易和区块暴露出去。只有没有 `--peer` 的进程调用 `Mine`。B 把交易先在本机准入，再原样转发给 A。

**Tech Stack:** Go 1.26.1，`golang.org/x/crypto/sha3` 的 `NewLegacyKeccak256`，`github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa` 的 `SignCompact` / `RecoverCompact`。

**Spec:** `docs/superpowers/specs/2026-10-29-minichain-design.md`

## Global Constraints

- 模块路径：`github.com/slmthydesp/minichain`
- `go` 版本：1.26.1
- 远程：`git@github.com:slmthydesp/minichain.git`，分支 `main`
- 工作副本：go-ethereum 的 `golang_demo/minichain`
- 依赖只加 `golang.org/x/crypto/sha3` 和 `github.com/decred/dcrd/dcrec/secp256k1/v4`
- 不依赖 go-ethereum，不做 EVM、智能合约、gas、点对点节点发现、持久化数据库、Merkle Patricia Trie、EIP-155
- 只有 A 出块，B 不出块，也不做分叉选择
- 测试不调用 `time.Sleep`
- 每个任务结束时在 minichain 仓库提交一次。不提交 go-ethereum 父仓库
- 全部任务完成后再 `git push origin main`
- 整数用大端。JSON 十六进制用小写，可以带 `0x` 前缀
- 演示私钥：`ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80`
- 该私钥地址：`f39fd6e51aad88f6f4ce6ab8827279cfffb92266`
- 创世账户余额：`1000000`，nonce `0`
- 空状态根：`c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470`

## File structure

- `chain/address.go` — `Address`、`Hash`、十六进制
- `chain/tx.go` — 交易、签名原文、规范字节、`Sign`、`Sender`
- `chain/block.go` — 区块和区块哈希
- `chain/hash.go` — 把 Keccak 结果收成 `Hash`
- `chain/errors.go` — 哨兵错误
- `chain/pool.go` — 内存交易池
- `chain/state.go` — 账户表、复制、状态根
- `chain/apply.go` — 执行一笔转账
- `chain/chain.go` — 创世、准入、出块、接受块
- `chain/types_test.go`、`pool_test.go`、`apply_test.go`、`state_test.go`、`chain_test.go`
- `sig/sig.go` — Keccak、签名、恢复、校验
- `sig/sig_test.go`
- `node/node.go` — 跟随者的一次拉取
- `node/http.go` — `POST /tx`、`GET /status`、`GET /blocks`
- `node/node_test.go`
- `cmd/node/main.go` — 进程 A 或 B
- `cmd/send/main.go` — 签名并提交
- `README.md`

`sig` 不 import `chain`。`chain` 可以 import `sig`。`node` import `chain`。

---

### Task 1: 账户、交易、区块

**Files:**
- Create: `go.mod`
- Create: `chain/address.go`
- Create: `chain/tx.go`
- Create: `chain/block.go`
- Create: `chain/hash.go`
- Test: `chain/types_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  - `type Address [20]byte`
  - `func ParseAddress(s string) (Address, error)`
  - `func (a Address) Hex() string`
  - `type Hash [32]byte`
  - `func ParseHash(s string) (Hash, error)`
  - `func (h Hash) Hex() string`
  - `type Account struct { Address Address; Balance uint64; Nonce uint64 }`
  - `type Tx struct { To Address; Value uint64; Nonce uint64; Sig []byte }`
  - `func (tx Tx) SigningBytes() []byte`
  - `func (tx Tx) CanonicalBytes() []byte`
  - `type Block struct { Height uint64; ParentHash Hash; Txs []Tx; StateRoot Hash }`
  - `func (b Block) Hash() Hash`

- [ ] **Step 1: Write the failing test**

在 `golang_demo/minichain` 执行：

```bash
go mod init github.com/slmthydesp/minichain
```

把 `go.mod` 的 go 行改成 `go 1.26.1`。

创建 `chain/types_test.go`：

```go
package chain

import (
	"bytes"
	"testing"
)

func TestAccountFieldsRoundTrip(t *testing.T) {
	var addr Address
	addr[19] = 7
	got := Account{Address: addr, Balance: 42, Nonce: 3}
	if got.Address != addr || got.Balance != 42 || got.Nonce != 3 {
		t.Fatalf("account = %+v", got)
	}
}

func TestTxSigningAndCanonicalBytes(t *testing.T) {
	var to Address
	to[19] = 1
	tx := Tx{To: to, Value: 9, Nonce: 4, Sig: bytes.Repeat([]byte{0xab}, 65)}
	sign := tx.SigningBytes()
	if len(sign) != 36 {
		t.Fatalf("signing bytes len = %d", len(sign))
	}
	canon := tx.CanonicalBytes()
	if len(canon) != 101 {
		t.Fatalf("canonical len = %d", len(canon))
	}
	if !bytes.Equal(canon[:36], sign) || !bytes.Equal(canon[36:], tx.Sig) {
		t.Fatal("canonical bytes are not signing bytes plus signature")
	}
	if sign[19] != 1 || sign[27] != 9 || sign[35] != 4 {
		t.Fatalf("signing layout = %x", sign)
	}
}

func TestParseAddressAndHash(t *testing.T) {
	addr, err := ParseAddress("0xF39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	if err != nil {
		t.Fatal(err)
	}
	if addr.Hex() != "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266" {
		t.Fatalf("address hex = %s", addr.Hex())
	}
	const hexHash = "abababababababababababababababababababababababababababababababab"
	h, err := ParseHash("0x" + hexHash)
	if err != nil {
		t.Fatal(err)
	}
	if h.Hex() != "0x"+hexHash {
		t.Fatalf("hash hex = %s", h.Hex())
	}
}

func TestBlockHashChangesWhenContentsChange(t *testing.T) {
	var to Address
	to[19] = 1
	base := Block{
		Height:     1,
		ParentHash: Hash{1},
		StateRoot:  Hash{2},
		Txs: []Tx{{
			To: to, Value: 1, Nonce: 0,
			Sig: bytes.Repeat([]byte{3}, 65),
		}},
	}
	same := base
	if base.Hash() != same.Hash() {
		t.Fatal("same contents produced different hashes")
	}
	parent := base
	parent.ParentHash[0] ^= 0x01
	root := base
	root.StateRoot[0] ^= 0x01
	txs := base
	txs.Txs[0].Sig[0] ^= 0x01
	if base.Hash() == parent.Hash() || base.Hash() == root.Hash() || base.Hash() == txs.Hash() {
		t.Fatal("hash did not change when parent, state root, or transaction changed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./chain -run 'TestAccountFieldsRoundTrip|TestTxSigningAndCanonicalBytes|TestParseAddressAndHash|TestBlockHashChangesWhenContentsChange' -count=1`

Expected: FAIL，编译错误，`undefined: Account` 或 `undefined: ParseAddress`

- [ ] **Step 3: Write minimal implementation**

```bash
go get golang.org/x/crypto/sha3
```

`chain/address.go`：

```go
package chain

import (
	"encoding/hex"
	"fmt"
	"strings"
)

type Address [20]byte

func (a Address) Hex() string {
	return "0x" + hex.EncodeToString(a[:])
}

func ParseAddress(s string) (Address, error) {
	b, err := decodeHex(s)
	if err != nil {
		return Address{}, err
	}
	if len(b) != 20 {
		return Address{}, fmt.Errorf("address length %d", len(b))
	}
	var a Address
	copy(a[:], b)
	return a, nil
}

type Hash [32]byte

func (h Hash) Hex() string {
	return "0x" + hex.EncodeToString(h[:])
}

func ParseHash(s string) (Hash, error) {
	b, err := decodeHex(s)
	if err != nil {
		return Hash{}, err
	}
	if len(b) != 32 {
		return Hash{}, fmt.Errorf("hash length %d", len(b))
	}
	var h Hash
	copy(h[:], b)
	return h, nil
}

func decodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	return hex.DecodeString(s)
}
```

`chain/tx.go`：

```go
package chain

import "encoding/binary"

type Account struct {
	Address Address
	Balance uint64
	Nonce   uint64
}

type Tx struct {
	To    Address
	Value uint64
	Nonce uint64
	Sig   []byte
}

func (tx Tx) SigningBytes() []byte {
	buf := make([]byte, 36)
	copy(buf[:20], tx.To[:])
	binary.BigEndian.PutUint64(buf[20:28], tx.Value)
	binary.BigEndian.PutUint64(buf[28:36], tx.Nonce)
	return buf
}

func (tx Tx) CanonicalBytes() []byte {
	buf := make([]byte, 36+len(tx.Sig))
	copy(buf, tx.SigningBytes())
	copy(buf[36:], tx.Sig)
	return buf
}
```

`chain/hash.go`：

```go
package chain

import "golang.org/x/crypto/sha3"

func keccak(data []byte) Hash {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(data)
	sum := h.Sum(nil)
	var out Hash
	copy(out[:], sum)
	return out
}
```

`chain/block.go`：

```go
package chain

import "encoding/binary"

type Block struct {
	Height     uint64
	ParentHash Hash
	Txs        []Tx
	StateRoot  Hash
}

func (b Block) Hash() Hash {
	buf := make([]byte, 0, 68+len(b.Txs)*101)
	buf = append(buf, b.ParentHash[:]...)
	buf = append(buf, b.StateRoot[:]...)
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b.Txs)))
	buf = append(buf, n[:]...)
	for _, tx := range b.Txs {
		buf = append(buf, tx.CanonicalBytes()...)
	}
	return keccak(buf)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./chain -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w chain/address.go chain/tx.go chain/hash.go chain/block.go chain/types_test.go
git add go.mod go.sum chain/address.go chain/tx.go chain/hash.go chain/block.go chain/types_test.go
git commit -m "$(cat <<'EOF'
chain: add account, transaction, and block types

EOF
)"
```

---

### Task 2: Keccak-256 和 secp256k1

**Files:**
- Create: `sig/sig.go`
- Create: `sig/sig_test.go`
- Modify: `chain/hash.go`
- Modify: `chain/tx.go`

**Interfaces:**
- Consumes: `Tx.SigningBytes`
- Produces:
  - `const DemoKeyHex = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"`
  - `func Keccak(data []byte) [32]byte`
  - `func ParseKey(hexKey string) ([]byte, error)`
  - `func KeyAddress(key []byte) ([20]byte, error)`
  - `func Sign(key, hash []byte) ([]byte, error)` 在包 `sig`
  - `func Verify(addr [20]byte, hash, signature []byte) error`
  - `func Recover(hash, signature []byte) ([20]byte, error)`
  - `func Sign(key []byte, to Address, value, nonce uint64) (Tx, error)` 在包 `chain`

- [ ] **Step 1: Write the failing test**

创建 `sig/sig_test.go`：

```go
package sig

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestDemoKeyAddress(t *testing.T) {
	key, err := ParseKey(DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	got, err := KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString("f39fd6e51aad88f6f4ce6ab8827279cfffb92266")
	if !bytes.Equal(got[:], want) {
		t.Fatalf("address = %x", got)
	}
}

func TestVerifyRejectsFlippedSignatureByte(t *testing.T) {
	key, err := ParseKey(DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	hash := Keccak([]byte("minichain"))
	signature, err := Sign(key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(addr, hash[:], signature); err != nil {
		t.Fatal(err)
	}
	flipped := bytes.Clone(signature)
	flipped[10] ^= 0x01
	if err := Verify(addr, hash[:], flipped); err == nil {
		t.Fatal("expected flipped signature to fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./sig -count=1`

Expected: FAIL，找不到包 `sig`，或 `undefined: ParseKey`

- [ ] **Step 3: Write minimal implementation**

```bash
go get github.com/decred/dcrd/dcrec/secp256k1/v4
```

`sig/sig.go`：

```go
package sig

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

const DemoKeyHex = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func Keccak(data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(data)
	sum := h.Sum(nil)
	var out [32]byte
	copy(out[:], sum)
	return out
}

func ParseKey(hexKey string) ([]byte, error) {
	hexKey = strings.TrimPrefix(strings.TrimPrefix(hexKey, "0x"), "0X")
	b, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, err
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("key length %d", len(b))
	}
	return b, nil
}

func KeyAddress(key []byte) ([20]byte, error) {
	if len(key) != 32 {
		return [20]byte{}, fmt.Errorf("key length %d", len(key))
	}
	pub := secp256k1.PrivKeyFromBytes(key).PubKey().SerializeUncompressed()
	sum := Keccak(pub[1:])
	var addr [20]byte
	copy(addr[:], sum[12:])
	return addr, nil
}

func Sign(key, hash []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key length %d", len(key))
	}
	if len(hash) != 32 {
		return nil, fmt.Errorf("hash length %d", len(hash))
	}
	priv := secp256k1.PrivKeyFromBytes(key)
	return ecdsa.SignCompact(priv, hash, false), nil
}

func Recover(hash, signature []byte) ([20]byte, error) {
	if len(signature) != 65 {
		return [20]byte{}, errors.New("bad signature")
	}
	pub, _, err := ecdsa.RecoverCompact(signature, hash)
	if err != nil {
		return [20]byte{}, err
	}
	raw := pub.SerializeUncompressed()
	sum := Keccak(raw[1:])
	var addr [20]byte
	copy(addr[:], sum[12:])
	return addr, nil
}

func Verify(addr [20]byte, hash, signature []byte) error {
	got, err := Recover(hash, signature)
	if err != nil {
		return err
	}
	if got != addr {
		return errors.New("signature mismatch")
	}
	return nil
}
```

把 `chain/hash.go` 换成：

```go
package chain

import "github.com/slmthydesp/minichain/sig"

func keccak(data []byte) Hash {
	sum := sig.Keccak(data)
	var out Hash
	copy(out[:], sum[:])
	return out
}
```

`chain/tx.go` 增加 import `"github.com/slmthydesp/minichain/sig"`，并在文件末尾增加：

```go
func Sign(key []byte, to Address, value, nonce uint64) (Tx, error) {
	tx := Tx{To: to, Value: value, Nonce: nonce}
	sum := keccak(tx.SigningBytes())
	s, err := sig.Sign(key, sum[:])
	if err != nil {
		return Tx{}, err
	}
	tx.Sig = s
	return tx, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./sig ./chain -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w sig/sig.go sig/sig_test.go chain/hash.go chain/tx.go
git add go.mod go.sum sig/sig.go sig/sig_test.go chain/hash.go chain/tx.go
git commit -m "$(cat <<'EOF'
sig: verify keccak and secp256k1 signatures

EOF
)"
```

---

### Task 3: 内存交易池

**Files:**
- Create: `chain/errors.go`
- Create: `chain/pool.go`
- Modify: `chain/tx.go`
- Test: `chain/pool_test.go`

**Interfaces:**
- Consumes: `chain.Sign`，`sig.Recover`，`Tx.SigningBytes`
- Produces:
  - `var ErrBadSignature, ErrNonceTooLow, ErrInsufficientBalance, ErrNonceUsed error`
  - `func (tx Tx) Sender() (Address, error)`
  - `func NewPool() *Pool`
  - `func (p *Pool) Add(tx Tx, balance, nonce uint64) error`
  - `func (p *Pool) Pending() []Tx`
  - `func (p *Pool) Drop(sender Address, nonce uint64)`

- [ ] **Step 1: Write the failing test**

创建 `chain/pool_test.go`：

```go
package chain

import (
	"bytes"
	"errors"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

func demoKey(t *testing.T) []byte {
	t.Helper()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func otherKey() []byte {
	key := make([]byte, 32)
	key[31] = 1
	return key
}

func TestPoolRejectsBadSignatureNonceAndBalance(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 1
	good, err := Sign(key, to, 10, 1)
	if err != nil {
		t.Fatal(err)
	}

	badSig := good
	badSig.Sig = bytes.Clone(good.Sig)
	badSig.Sig[0] = 0
	pool := NewPool()
	if err := pool.Add(badSig, 100, 0); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("bad signature err = %v", err)
	}

	low, err := Sign(key, to, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(low, 100, 1); !errors.Is(err, ErrNonceTooLow) {
		t.Fatalf("low nonce err = %v", err)
	}

	poor, err := Sign(key, to, 50, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(poor, 40, 1); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("balance err = %v", err)
	}
}

func TestPoolOrdersBySenderThenNonce(t *testing.T) {
	var to Address
	to[19] = 2
	demo, err := Sign(demoKey(t), to, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	demoFirst, err := Sign(demoKey(t), to, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Sign(otherKey(), to, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewPool()
	for _, tx := range []Tx{demo, other, demoFirst} {
		if err := pool.Add(tx, 100, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.Add(demoFirst, 100, 0); !errors.Is(err, ErrNonceUsed) {
		t.Fatalf("duplicate err = %v", err)
	}
	got := pool.Pending()
	if len(got) != 3 {
		t.Fatalf("pending = %d", len(got))
	}
	senders := make([]Address, len(got))
	for i, tx := range got {
		senders[i], err = tx.Sender()
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			cmp := bytes.Compare(senders[i-1][:], senders[i][:])
			if cmp > 0 || (cmp == 0 && got[i-1].Nonce > tx.Nonce) {
				t.Fatalf("order broken at %d", i)
			}
		}
	}
	pool.Drop(senders[0], got[0].Nonce)
	if len(pool.Pending()) != 2 {
		t.Fatalf("after drop pending = %d", len(pool.Pending()))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./chain -run 'TestPool' -count=1`

Expected: FAIL，`undefined: NewPool`

- [ ] **Step 3: Write minimal implementation**

`chain/errors.go`：

```go
package chain

import "errors"

var (
	ErrBadSignature        = errors.New("bad signature")
	ErrNonceTooLow         = errors.New("nonce too low")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrNonceUsed           = errors.New("nonce used")
)
```

在 `chain/tx.go` 增加：

```go
func (tx Tx) Sender() (Address, error) {
	sum := keccak(tx.SigningBytes())
	raw, err := sig.Recover(sum[:], tx.Sig)
	if err != nil {
		return Address{}, ErrBadSignature
	}
	var addr Address
	copy(addr[:], raw[:])
	return addr, nil
}
```

`chain/pool.go`：

```go
package chain

import (
	"bytes"
	"sort"
)

type pooled struct {
	sender Address
	tx     Tx
}

type Pool struct {
	items []pooled
}

func NewPool() *Pool {
	return &Pool{}
}

func (p *Pool) Add(tx Tx, balance, nonce uint64) error {
	sender, err := tx.Sender()
	if err != nil {
		return ErrBadSignature
	}
	if tx.Nonce < nonce {
		return ErrNonceTooLow
	}
	if tx.Value > balance {
		return ErrInsufficientBalance
	}
	for _, it := range p.items {
		if it.sender == sender && it.tx.Nonce == tx.Nonce {
			return ErrNonceUsed
		}
	}
	p.items = append(p.items, pooled{sender: sender, tx: tx})
	return nil
}

func (p *Pool) Pending() []Tx {
	items := append([]pooled(nil), p.items...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].sender != items[j].sender {
			return bytes.Compare(items[i].sender[:], items[j].sender[:]) < 0
		}
		return items[i].tx.Nonce < items[j].tx.Nonce
	})
	out := make([]Tx, len(items))
	for i, it := range items {
		out[i] = it.tx
	}
	return out
}

func (p *Pool) Drop(sender Address, nonce uint64) {
	kept := p.items[:0]
	for _, it := range p.items {
		if it.sender == sender && it.tx.Nonce == nonce {
			continue
		}
		kept = append(kept, it)
	}
	p.items = kept
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./chain -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w chain/errors.go chain/pool.go chain/tx.go chain/pool_test.go
git add chain/errors.go chain/pool.go chain/tx.go chain/pool_test.go
git commit -m "$(cat <<'EOF'
chain: reject invalid transactions in the mempool

EOF
)"
```

---

### Task 4: 执行一笔转账

**Files:**
- Create: `chain/state.go`
- Create: `chain/apply.go`
- Modify: `chain/errors.go`
- Test: `chain/apply_test.go`

**Interfaces:**
- Consumes: `Tx.Sender`，`ErrBadSignature`，`ErrInsufficientBalance`
- Produces:
  - `func NewState() *State`
  - `func (s *State) Copy() *State`
  - `func (s *State) Get(a Address) (Account, bool)`
  - `func (s *State) Put(a Account)`
  - `func (s *State) BalanceNonce(a Address) (balance, nonce uint64)`
  - `func Apply(s *State, tx Tx) error`
  - `var ErrNonceMismatch, ErrBalanceOverflow error`
- 收款方和发送方不是同一地址时，先检查 `recipient.Balance+tx.Value` 会不会溢出，溢出就返回 `ErrBalanceOverflow`，状态不动。同一地址时余额保持原值，nonce 加 1。自己转给自己没有把余额增加，所以不做这个加法溢出检查。

- [ ] **Step 1: Write the failing test**

创建 `chain/apply_test.go`：

```go
package chain

import (
	"errors"
	"math"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

func funded(t *testing.T, balance, nonce uint64) (*State, Address) {
	t.Helper()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	var addr Address
	copy(addr[:], raw[:])
	st := NewState()
	st.Put(Account{Address: addr, Balance: balance, Nonce: nonce})
	return st, addr
}

func TestApplyTransferUpdatesNonceAndBalances(t *testing.T) {
	st, from := funded(t, 1000, 0)
	var to Address
	to[19] = 9
	tx, err := Sign(demoKey(t), to, 40, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Get(from)
	if !ok || got.Balance != 960 || got.Nonce != 1 {
		t.Fatalf("sender = %+v ok=%v", got, ok)
	}
	recip, ok := st.Get(to)
	if !ok || recip.Balance != 40 || recip.Nonce != 0 {
		t.Fatalf("recipient = %+v ok=%v", recip, ok)
	}
}

func TestApplySelfTransferKeepsBalance(t *testing.T) {
	st, from := funded(t, 100, 0)
	tx, err := Sign(demoKey(t), from, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(from)
	if got.Balance != 100 || got.Nonce != 1 {
		t.Fatalf("self = %+v", got)
	}
}

func TestApplyRejectsWithoutMutating(t *testing.T) {
	st, from := funded(t, 10, 1)
	var to Address
	to[19] = 3
	before, _ := st.Get(from)

	tests := []struct {
		name string
		tx   func() Tx
		want error
	}{
		{"nonce", func() Tx {
			tx, err := Sign(demoKey(t), to, 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			return tx
		}, ErrNonceMismatch},
		{"balance", func() Tx {
			tx, err := Sign(demoKey(t), to, 11, 1)
			if err != nil {
				t.Fatal(err)
			}
			return tx
		}, ErrInsufficientBalance},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Apply(st, tt.tx())
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v", err)
			}
			got, _ := st.Get(from)
			if got != before {
				t.Fatalf("state changed to %+v", got)
			}
		})
	}
}

func TestApplyRejectsRecipientOverflow(t *testing.T) {
	st, _ := funded(t, 10, 0)
	var to Address
	to[19] = 4
	st.Put(Account{Address: to, Balance: math.MaxUint64 - 1, Nonce: 0})
	tx, err := Sign(demoKey(t), to, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(st, tx); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("err = %v", err)
	}
	got, _ := st.Get(to)
	if got.Balance != math.MaxUint64-1 {
		t.Fatalf("recipient balance = %d", got.Balance)
	}
}
```

`demoKey` 在 `chain/pool_test.go`，同一个包可以直接调用。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./chain -run 'TestApply' -count=1`

Expected: FAIL，`undefined: NewState` 或 `undefined: Apply`

- [ ] **Step 3: Write minimal implementation**

在 `chain/errors.go` 的 var 块追加：

```go
ErrNonceMismatch   = errors.New("nonce mismatch")
ErrBalanceOverflow = errors.New("balance overflow")
```

`chain/state.go`：

```go
package chain

type State struct {
	accounts map[Address]Account
}

func NewState() *State {
	return &State{accounts: make(map[Address]Account)}
}

func (s *State) Copy() *State {
	next := NewState()
	for k, v := range s.accounts {
		next.accounts[k] = v
	}
	return next
}

func (s *State) Get(a Address) (Account, bool) {
	acct, ok := s.accounts[a]
	return acct, ok
}

func (s *State) Put(a Account) {
	s.accounts[a.Address] = a
}

func (s *State) BalanceNonce(a Address) (uint64, uint64) {
	acct, ok := s.accounts[a]
	if !ok {
		return 0, 0
	}
	return acct.Balance, acct.Nonce
}
```

`chain/apply.go`：

```go
package chain

func Apply(s *State, tx Tx) error {
	sender, err := tx.Sender()
	if err != nil {
		return ErrBadSignature
	}
	from, ok := s.Get(sender)
	if !ok {
		from = Account{Address: sender}
	}
	if tx.Nonce != from.Nonce {
		return ErrNonceMismatch
	}
	if tx.Value > from.Balance {
		return ErrInsufficientBalance
	}
	if tx.To == sender {
		from.Nonce++
		s.Put(from)
		return nil
	}
	to, ok := s.Get(tx.To)
	if !ok {
		to = Account{Address: tx.To}
	}
	if to.Balance > ^uint64(0)-tx.Value {
		return ErrBalanceOverflow
	}
	from.Nonce++
	from.Balance -= tx.Value
	to.Balance += tx.Value
	s.Put(from)
	s.Put(to)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./chain -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w chain/errors.go chain/state.go chain/apply.go chain/apply_test.go
git add chain/errors.go chain/state.go chain/apply.go chain/apply_test.go
git commit -m "$(cat <<'EOF'
chain: apply a transfer

EOF
)"
```

---

### Task 5: 状态根

**Files:**
- Modify: `chain/state.go`
- Test: `chain/state_test.go`

**Interfaces:**
- Consumes: `NewState`，`Put`，`keccak`
- Produces:
  - `func (s *State) Delete(a Address)`
  - `func (s *State) Root() Hash`

- [ ] **Step 1: Write the failing test**

创建 `chain/state_test.go`：

```go
package chain

import "testing"

func TestEmptyStateRoot(t *testing.T) {
	want := keccak(nil)
	if NewState().Root() != want {
		t.Fatalf("empty root = %s", NewState().Root().Hex())
	}
	parsed, err := ParseHash("0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470")
	if err != nil {
		t.Fatal(err)
	}
	if want != parsed {
		t.Fatalf("keccak(nil) = %s", want.Hex())
	}
}

func TestStateRootChangesOnPutModifyDelete(t *testing.T) {
	a := Account{Address: Address{19: 1}, Balance: 5, Nonce: 1}
	b := Account{Address: Address{19: 2}, Balance: 0, Nonce: 0}
	left := NewState()
	left.Put(a)
	left.Put(b)
	right := NewState()
	right.Put(b)
	right.Put(a)
	if left.Root() != right.Root() {
		t.Fatal("insertion order changed the root")
	}
	empty := NewState().Root()
	if left.Root() == empty {
		t.Fatal("insert did not change the root")
	}
	modified := left.Copy()
	a.Balance = 6
	modified.Put(a)
	if modified.Root() == left.Root() {
		t.Fatal("modify did not change the root")
	}
	deleted := left.Copy()
	deleted.Delete(b.Address)
	if deleted.Root() == left.Root() {
		t.Fatal("delete did not change the root")
	}
	again := deleted.Root()
	deleted.Delete(Address{19: 9})
	if deleted.Root() != again {
		t.Fatal("deleting a missing account changed the root")
	}
	zero := NewState()
	zero.Put(b)
	if zero.Root() == empty {
		t.Fatal("zero-balance account was omitted")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./chain -run 'TestEmptyStateRoot|TestStateRootChangesOnPutModifyDelete' -count=1`

Expected: FAIL，`s.Root undefined` 或 `s.Delete undefined`

- [ ] **Step 3: Write minimal implementation**

`chain/state.go` 增加 import `"bytes"`、`"encoding/binary"`、`"sort"`，并增加：

```go
func (s *State) Delete(a Address) {
	delete(s.accounts, a)
}

func (s *State) Root() Hash {
	addrs := make([]Address, 0, len(s.accounts))
	for addr := range s.accounts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return bytes.Compare(addrs[i][:], addrs[j][:]) < 0
	})
	buf := make([]byte, 0, len(addrs)*36)
	var n [8]byte
	for _, addr := range addrs {
		acct := s.accounts[addr]
		buf = append(buf, acct.Address[:]...)
		binary.BigEndian.PutUint64(n[:], acct.Balance)
		buf = append(buf, n[:]...)
		binary.BigEndian.PutUint64(n[:], acct.Nonce)
		buf = append(buf, n[:]...)
	}
	return keccak(buf)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./chain -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w chain/state.go chain/state_test.go
git add chain/state.go chain/state_test.go
git commit -m "$(cat <<'EOF'
chain: hash account state

EOF
)"
```

---

### Task 6: 出块和接受块

**Files:**
- Create: `chain/chain.go`
- Test: `chain/chain_test.go`

**Interfaces:**
- Consumes: `NewState`，`Put`，`Root`，`Apply`，`NewPool`，`Pool.Add`，`Pool.Pending`，`Pool.Drop`，`Block.Hash`，`sig.DemoKeyHex`
- Produces:
  - `const GenesisBalance uint64 = 1000000`
  - `func New() *Chain`
  - `func (c *Chain) Head() Block`
  - `func (c *Chain) AddTx(tx Tx) (Address, error)`
  - `func (c *Chain) DropTx(sender Address, nonce uint64)`
  - `func (c *Chain) Mine() (Block, error)`
  - `func (c *Chain) Append(b Block) error`
  - `func (c *Chain) BlocksAfter(height uint64) []Block`
  - `func (c *Chain) Account(a Address) (Account, bool)`
  - `func (c *Chain) Pool() *Pool`
- `Mine` 和 `Append` 成功后，块里的交易从池里 `Drop`。`Apply` 失败或 nonce 不连续时，这笔和该发送方后面的交易留在池里。池为空也出空块。
- `Append` 在父哈希不是当前头、重放后的状态根不一致、或高度不是头高度加 1 时返回错误，链和状态保持原样。高度检查用来让两边的 `height` 对得上。
- `AddTx`、`DropTx`、`Mine`、`Append`、`Head`、`Account`、`BlocksAfter` 共用一把 `sync.Mutex`。B 转发失败时调用 `DropTx`，避免和 `Append` 同时改池。

- [ ] **Step 1: Write the failing test**

创建 `chain/chain_test.go`：

```go
package chain

import (
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

func TestNewGenesisFundsDemoAccount(t *testing.T) {
	c := New()
	head := c.Head()
	if head.Height != 0 || head.ParentHash != (Hash{}) || len(head.Txs) != 0 {
		t.Fatalf("genesis = %+v", head)
	}
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	var addr Address
	copy(addr[:], raw[:])
	acct, ok := c.Account(addr)
	if !ok || acct.Balance != GenesisBalance || acct.Nonce != 0 {
		t.Fatalf("demo = %+v ok=%v", acct, ok)
	}
	st := NewState()
	st.Put(acct)
	if head.StateRoot != st.Root() {
		t.Fatalf("genesis root = %s", head.StateRoot.Hex())
	}
}

func TestMineAndAppend(t *testing.T) {
	c := New()
	parent := c.Head().Hash()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[19] = 8
	tx, err := Sign(key, to, 15, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine()
	if err != nil {
		t.Fatal(err)
	}
	if block.Height != 1 || block.ParentHash != parent || len(block.Txs) != 1 {
		t.Fatalf("mined = %+v", block)
	}
	if len(c.Pool().Pending()) != 0 {
		t.Fatal("mined transaction still pending")
	}
	acct, _ := c.Account(to)
	if acct.Balance != 15 {
		t.Fatalf("recipient balance = %d", acct.Balance)
	}

	follower := New()
	if err := follower.Append(block); err != nil {
		t.Fatal(err)
	}
	if follower.Head().Hash() != c.Head().Hash() || follower.Head().StateRoot != c.Head().StateRoot {
		t.Fatal("follower diverged")
	}

	badParent := block
	badParent.ParentHash[0] ^= 0x01
	if err := New().Append(badParent); err == nil {
		t.Fatal("expected bad parent to fail")
	}
	badRoot := block
	badRoot.StateRoot[0] ^= 0x01
	fresh := New()
	if err := fresh.Append(badRoot); err == nil {
		t.Fatal("expected bad root to fail")
	}
	if fresh.Head().Height != 0 {
		t.Fatal("rejected block was kept")
	}
}

func TestMineLeavesGappedNonce(t *testing.T) {
	c := New()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[0] = 1
	future, err := Sign(key, to, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(future); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine()
	if err != nil {
		t.Fatal(err)
	}
	if block.Height != 1 || len(block.Txs) != 0 {
		t.Fatalf("empty block = height %d txs %d", block.Height, len(block.Txs))
	}
	if len(c.Pool().Pending()) != 1 {
		t.Fatal("gapped transaction was dropped")
	}
}

func TestAppendDropsIncludedTx(t *testing.T) {
	producer := New()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[1] = 2
	tx, err := Sign(key, to, 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := producer.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := producer.Mine()
	if err != nil {
		t.Fatal(err)
	}
	follower := New()
	if _, err := follower.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	if err := follower.Append(block); err != nil {
		t.Fatal(err)
	}
	if len(follower.Pool().Pending()) != 0 {
		t.Fatal("appended transaction still pending")
	}
	if err := follower.Append(block); err == nil {
		t.Fatal("expected second append to fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./chain -run 'TestNewGenesisFundsDemoAccount|TestMineAndAppend|TestMineLeavesGappedNonce|TestAppendDropsIncludedTx' -count=1`

Expected: FAIL，`undefined: New`

- [ ] **Step 3: Write minimal implementation**

`chain/chain.go`：

```go
package chain

import (
	"errors"
	"sync"

	"github.com/slmthydesp/minichain/sig"
)

const GenesisBalance uint64 = 1000000

type Chain struct {
	mu     sync.Mutex
	blocks []Block
	state  *State
	pool   *Pool
}

func New() *Chain {
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		panic(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		panic(err)
	}
	var addr Address
	copy(addr[:], raw[:])
	st := NewState()
	st.Put(Account{Address: addr, Balance: GenesisBalance, Nonce: 0})
	block := Block{Height: 0, StateRoot: st.Root()}
	return &Chain{
		blocks: []Block{block},
		state:  st,
		pool:   NewPool(),
	}
}

func (c *Chain) Head() Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.blocks[len(c.blocks)-1]
}

func (c *Chain) Account(a Address) (Account, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.Get(a)
}

func (c *Chain) Pool() *Pool {
	return c.pool
}

func (c *Chain) AddTx(tx Tx) (Address, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sender, err := tx.Sender()
	if err != nil {
		return Address{}, ErrBadSignature
	}
	balance, nonce := c.state.BalanceNonce(sender)
	if err := c.pool.Add(tx, balance, nonce); err != nil {
		return Address{}, err
	}
	return sender, nil
}

func (c *Chain) DropTx(sender Address, nonce uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pool.Drop(sender, nonce)
}

func (c *Chain) BlocksAfter(height uint64) []Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []Block{}
	for _, block := range c.blocks {
		if block.Height > height {
			out = append(out, block)
		}
	}
	return out
}

func (c *Chain) Mine() (Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := c.pool.Pending()
	scratch := c.state.Copy()
	chosen := make([]Tx, 0)
	for i := 0; i < len(pending); {
		sender, err := pending[i].Sender()
		if err != nil {
			return Block{}, err
		}
		_, nonce := scratch.BalanceNonce(sender)
		j := i
		for j < len(pending) {
			txSender, err := pending[j].Sender()
			if err != nil {
				return Block{}, err
			}
			if txSender != sender {
				break
			}
			if pending[j].Nonce != nonce {
				break
			}
			if err := Apply(scratch, pending[j]); err != nil {
				break
			}
			chosen = append(chosen, pending[j])
			nonce++
			j++
		}
		for j < len(pending) {
			txSender, err := pending[j].Sender()
			if err != nil {
				return Block{}, err
			}
			if txSender != sender {
				break
			}
			j++
		}
		i = j
	}
	for _, tx := range chosen {
		sender, err := tx.Sender()
		if err != nil {
			return Block{}, err
		}
		c.pool.Drop(sender, tx.Nonce)
	}
	head := c.blocks[len(c.blocks)-1]
	block := Block{
		Height:     head.Height + 1,
		ParentHash: head.Hash(),
		Txs:        chosen,
		StateRoot:  scratch.Root(),
	}
	c.blocks = append(c.blocks, block)
	c.state = scratch
	return block, nil
}

func (c *Chain) Append(b Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	head := c.blocks[len(c.blocks)-1]
	if b.ParentHash != head.Hash() {
		return errors.New("bad parent hash")
	}
	if b.Height != head.Height+1 {
		return errors.New("bad height")
	}
	scratch := c.state.Copy()
	for _, tx := range b.Txs {
		if err := Apply(scratch, tx); err != nil {
			return err
		}
	}
	if scratch.Root() != b.StateRoot {
		return errors.New("bad state root")
	}
	c.blocks = append(c.blocks, b)
	c.state = scratch
	for _, tx := range b.Txs {
		sender, err := tx.Sender()
		if err != nil {
			return err
		}
		c.pool.Drop(sender, tx.Nonce)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./chain -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w chain/chain.go chain/chain_test.go
git add chain/chain.go chain/chain_test.go
git commit -m "$(cat <<'EOF'
chain: mine and append blocks

EOF
)"
```

---

### Task 7: 两个进程

**Files:**
- Create: `node/http.go`
- Create: `node/node.go`
- Create: `node/node_test.go`
- Create: `cmd/node/main.go`
- Create: `cmd/send/main.go`

**Interfaces:**
- Consumes: `chain.New`，`AddTx`，`DropTx`，`Mine`，`Append`，`BlocksAfter`，`Head`，`Pool`，`Sign`，`ParseAddress`，`ParseHash`
- Produces:
  - `func NewMiner(c *chain.Chain) *Node`
  - `func NewFollower(c *chain.Chain, peer string) *Node`
  - `func (n *Node) Handler() http.Handler`
  - `func (n *Node) FollowOnce(ctx context.Context) error`
  - `func (n *Node) Chain() *chain.Chain`

- [ ] **Step 1: Write the failing test**

创建 `node/node_test.go`：

```go
package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slmthydesp/minichain/chain"
	"github.com/slmthydesp/minichain/sig"
)

func signedBody(t *testing.T, nonce, value uint64) []byte {
	t.Helper()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to chain.Address
	to[19] = 1
	tx, err := chain.Sign(key, to, value, nonce)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"to":    tx.To.Hex(),
		"value": tx.Value,
		"nonce": tx.Nonce,
		"sig":   "0x" + hex.EncodeToString(tx.Sig),
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func postTx(t *testing.T, url string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url+"/tx", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestFollowerCatchesMinerAndExitsWhenMinerStops(t *testing.T) {
	minerChain := chain.New()
	miner := NewMiner(minerChain)
	srv := httptest.NewServer(miner.Handler())
	t.Cleanup(srv.Close)

	resp := postTx(t, srv.URL, signedBody(t, 0, 20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if _, err := minerChain.Mine(); err != nil {
		t.Fatal(err)
	}

	followerChain := chain.New()
	follower := NewFollower(followerChain, srv.URL)
	if err := follower.FollowOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if followerChain.Head().Height != minerChain.Head().Height {
		t.Fatalf("height follower=%d miner=%d", followerChain.Head().Height, minerChain.Head().Height)
	}
	if followerChain.Head().StateRoot != minerChain.Head().StateRoot {
		t.Fatal("state roots differ")
	}

	srv.Close()
	if err := follower.FollowOnce(context.Background()); err == nil {
		t.Fatal("expected error after miner stopped")
	}
}

func TestFollowerAcceptsTransactionAndForwardsIt(t *testing.T) {
	minerChain := chain.New()
	miner := NewMiner(minerChain)
	minerSrv := httptest.NewServer(miner.Handler())
	t.Cleanup(minerSrv.Close)

	followerChain := chain.New()
	follower := NewFollower(followerChain, minerSrv.URL)
	followerSrv := httptest.NewServer(follower.Handler())
	t.Cleanup(followerSrv.Close)

	resp := postTx(t, followerSrv.URL, signedBody(t, 0, 7))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(minerChain.Pool().Pending()) != 1 || len(followerChain.Pool().Pending()) != 1 {
		t.Fatalf("pools miner=%d follower=%d", len(minerChain.Pool().Pending()), len(followerChain.Pool().Pending()))
	}
	if _, err := minerChain.Mine(); err != nil {
		t.Fatal(err)
	}
	if err := follower.FollowOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if followerChain.Head().StateRoot != minerChain.Head().StateRoot {
		t.Fatal("state roots differ")
	}
	if len(minerChain.Pool().Pending()) != 0 || len(followerChain.Pool().Pending()) != 0 {
		t.Fatal("transaction remained in a pool")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./node -count=1`

Expected: FAIL，`undefined: NewMiner`

- [ ] **Step 3: Write minimal implementation**

`node/http.go`：

```go
package node

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/slmthydesp/minichain/chain"
)

type txJSON struct {
	To    string `json:"to"`
	Value uint64 `json:"value"`
	Nonce uint64 `json:"nonce"`
	Sig   string `json:"sig"`
}

type blockJSON struct {
	Height     uint64   `json:"height"`
	ParentHash string   `json:"parentHash"`
	StateRoot  string   `json:"stateRoot"`
	Hash       string   `json:"hash"`
	Txs        []txJSON `json:"txs"`
}

type statusJSON struct {
	Height    uint64 `json:"height"`
	StateRoot string `json:"stateRoot"`
	Head      string `json:"head"`
}

func (n *Node) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tx", n.handleTx)
	mux.HandleFunc("GET /status", n.handleStatus)
	if n.peer == "" {
		mux.HandleFunc("GET /blocks", n.handleBlocks)
	}
	return mux
}

func (n *Node) handleStatus(w http.ResponseWriter, _ *http.Request) {
	head := n.chain.Head()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statusJSON{
		Height:    head.Height,
		StateRoot: head.StateRoot.Hex(),
		Head:      head.Hash().Hex(),
	})
}

func (n *Node) handleBlocks(w http.ResponseWriter, r *http.Request) {
	var after uint64
	if raw := r.URL.Query().Get("after"); raw != "" {
		if _, err := fmt.Sscan(raw, &after); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	blocks := n.chain.BlocksAfter(after)
	out := make([]blockJSON, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, encodeBlock(block))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (n *Node) handleTx(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tx, err := decodeTx(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sender, err := n.chain.AddTx(tx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if n.peer == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	req, err := http.NewRequest(http.MethodPost, n.peer+"/tx", bytes.NewReader(body))
	if err != nil {
		n.chain.DropTx(sender, tx.Nonce)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		n.chain.DropTx(sender, tx.Nonce)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		n.chain.DropTx(sender, tx.Nonce)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func encodeBlock(block chain.Block) blockJSON {
	txs := make([]txJSON, len(block.Txs))
	for i, tx := range block.Txs {
		txs[i] = txJSON{
			To:    tx.To.Hex(),
			Value: tx.Value,
			Nonce: tx.Nonce,
			Sig:   "0x" + hex.EncodeToString(tx.Sig),
		}
	}
	return blockJSON{
		Height:     block.Height,
		ParentHash: block.ParentHash.Hex(),
		StateRoot:  block.StateRoot.Hex(),
		Hash:       block.Hash().Hex(),
		Txs:        txs,
	}
}

func decodeTx(body []byte) (chain.Tx, error) {
	var in txJSON
	if err := json.Unmarshal(body, &in); err != nil {
		return chain.Tx{}, err
	}
	to, err := chain.ParseAddress(in.To)
	if err != nil {
		return chain.Tx{}, err
	}
	sigBytes, err := decodeHex(in.Sig)
	if err != nil {
		return chain.Tx{}, err
	}
	return chain.Tx{To: to, Value: in.Value, Nonce: in.Nonce, Sig: sigBytes}, nil
}

func decodeBlock(in blockJSON) (chain.Block, error) {
	parent, err := chain.ParseHash(in.ParentHash)
	if err != nil {
		return chain.Block{}, err
	}
	root, err := chain.ParseHash(in.StateRoot)
	if err != nil {
		return chain.Block{}, err
	}
	txs := make([]chain.Tx, len(in.Txs))
	for i, item := range in.Txs {
		raw, err := json.Marshal(item)
		if err != nil {
			return chain.Block{}, err
		}
		txs[i], err = decodeTx(raw)
		if err != nil {
			return chain.Block{}, err
		}
	}
	block := chain.Block{Height: in.Height, ParentHash: parent, Txs: txs, StateRoot: root}
	if !strings.EqualFold(block.Hash().Hex(), in.Hash) {
		return chain.Block{}, errors.New("block hash mismatch")
	}
	return block, nil
}

func decodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	return hex.DecodeString(s)
}
```

`node/node.go`：

```go
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/slmthydesp/minichain/chain"
)

type Node struct {
	chain  *chain.Chain
	peer   string
	client *http.Client
}

func NewMiner(c *chain.Chain) *Node {
	return &Node{chain: c, client: http.DefaultClient}
}

func NewFollower(c *chain.Chain, peer string) *Node {
	return &Node{
		chain:  c,
		peer:   strings.TrimRight(peer, "/"),
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (n *Node) Chain() *chain.Chain {
	return n.chain
}

func (n *Node) FollowOnce(ctx context.Context) error {
	if n.peer == "" {
		return fmt.Errorf("not a follower")
	}
	url := fmt.Sprintf("%s/blocks?after=%d", n.peer, n.chain.Head().Height)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("blocks status %d: %s", resp.StatusCode, body)
	}
	var blocks []blockJSON
	if err := json.Unmarshal(body, &blocks); err != nil {
		return err
	}
	for _, item := range blocks {
		block, err := decodeBlock(item)
		if err != nil {
			return err
		}
		if err := n.chain.Append(block); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./node ./chain ./sig -count=1`

Expected: PASS

- [ ] **Step 5: Add the two commands**

`cmd/node/main.go`：

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/slmthydesp/minichain/chain"
	"github.com/slmthydesp/minichain/node"
)

func main() {
	listen := flag.String("listen", "", "listen address")
	interval := flag.Duration("interval", time.Second, "block interval")
	peer := flag.String("peer", "", "block producer URL")
	poll := flag.Duration("poll", 200*time.Millisecond, "block poll interval")
	flag.Parse()

	addr := *listen
	if addr == "" {
		addr = "127.0.0.1:8545"
		if *peer != "" {
			addr = "127.0.0.1:8546"
		}
	}
	c := chain.New()
	if *peer == "" {
		n := node.NewMiner(c)
		go func() {
			for {
				if _, err := c.Mine(); err != nil {
					fmt.Fprintf(os.Stderr, "mine: %v\n", err)
					os.Exit(1)
				}
				time.Sleep(*interval)
			}
		}()
		log.Fatal((&http.Server{Addr: addr, Handler: n.Handler()}).ListenAndServe())
	}

	n := node.NewFollower(c, *peer)
	srv := &http.Server{Addr: addr, Handler: n.Handler()}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "listen: %v\n", err)
			os.Exit(1)
		}
	}()
	for {
		if err := n.FollowOnce(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "sync: %v\n", err)
			os.Exit(1)
		}
		time.Sleep(*poll)
	}
}
```

`cmd/send/main.go`：

```go
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/slmthydesp/minichain/chain"
	"github.com/slmthydesp/minichain/sig"
)

func main() {
	url := flag.String("url", "", "node URL")
	toHex := flag.String("to", "", "recipient address")
	value := flag.Uint64("value", 0, "amount")
	nonceStr := flag.String("nonce", "", "account nonce")
	keyHex := flag.String("key", sig.DemoKeyHex, "hex private key")
	flag.Parse()
	if *url == "" || *toHex == "" || *nonceStr == "" {
		fmt.Fprintln(os.Stderr, "url, to, and nonce are required")
		os.Exit(1)
	}
	nonce, err := strconv.ParseUint(*nonceStr, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nonce: %v\n", err)
		os.Exit(1)
	}
	key, err := sig.ParseKey(*keyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "key: %v\n", err)
		os.Exit(1)
	}
	to, err := chain.ParseAddress(*toHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "to: %v\n", err)
		os.Exit(1)
	}
	tx, err := chain.Sign(key, to, *value, nonce)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: %v\n", err)
		os.Exit(1)
	}
	body, err := json.Marshal(map[string]any{
		"to":    tx.To.Hex(),
		"value": tx.Value,
		"nonce": tx.Nonce,
		"sig":   "0x" + hex.EncodeToString(tx.Sig),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "json: %v\n", err)
		os.Exit(1)
	}
	resp, err := http.Post(*url+"/tx", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "post: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		fmt.Fprintf(os.Stderr, "status %d: %s\n", resp.StatusCode, respBody)
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Run the commands compile**

Run: `go test ./...`，然后 `go build -o /dev/null ./cmd/node` 和 `go build -o /dev/null ./cmd/send`

Expected: 测试 PASS，两个命令编译成功

- [ ] **Step 7: Commit**

```bash
gofmt -w node/http.go node/node.go node/node_test.go cmd/node/main.go cmd/send/main.go
git add node/http.go node/node.go node/node_test.go cmd/node/main.go cmd/send/main.go
git commit -m "$(cat <<'EOF'
node: sync blocks and accept transactions on both processes

EOF
)"
```

---

### Task 8: README

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: `cmd/node` 的 `--listen`、`--peer`、`--interval`、`--poll`，`cmd/send` 的 `--url`、`--to`、`--value`、`--nonce`、`--key`
- Produces: 一份换一台机器可以照着跑的说明

- [ ] **Step 1: Replace README.md**

用下面的全文替换 `README.md`。这里用四个反引号包住，避免里面的命令块把围栏截断。

````markdown
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
````

- [ ] **Step 2: Run the tests once more**

Run: `go test ./...`

Expected: PASS

- [ ] **Step 3: Commit and push**

```bash
git add README.md
git commit -m "$(cat <<'EOF'
docs: explain how to run two nodes

EOF
)"
git push origin main
git status -sb
```

Expected: 工作区干净，`main` 没有超前远程的提交。不要在 go-ethereum 父仓库提交。
