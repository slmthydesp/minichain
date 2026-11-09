package chain

import (
	"bytes"
	"encoding/binary"

	"github.com/slmthydesp/minichain/sig"
)

// Account 是一个账户。Nonce 是这个账户已经发出的交易数，下一笔执行时必须等于它。
// Balance 按最小单位记账，1 个币是 Unit，也就是 10^18。宽度是 256 位。代码和存储放在 State 里，不放在这个结构上。
type Account struct {
	Address Address
	Balance Uint256
	Nonce   uint64
}

// ChainID 是本链唯一接受的链编号。它签进交易原文，用来挡住别的链上的重放。
const ChainID uint64 = 6666

// Tx 是一笔交易。Create 为真时不写 To，Data 是创建合约的初始代码。
// 否则 To 是 20 字节收款方，20 个 0(零地址) 也是收款方，Data 是调用数据。
// Sig 是对 SigningBytes 的可恢复签名，固定 65 字节。
type Tx struct {
	To       Address
	Create   bool
	Data     []byte
	Value    Uint256
	Nonce    uint64
	GasLimit uint64
	GasPrice uint64
	ChainID  uint64
	Sig      []byte
}

// Clone 复制这笔交易。Data 和 Sig 是切片，值拷贝仍共用底层数组，所以这两段单独拷一份。
func (tx Tx) Clone() Tx {
	tx.Data = bytes.Clone(tx.Data)
	tx.Sig = bytes.Clone(tx.Sig)
	return tx
}

// SigningBytes 是被签名的原文，整数都是大端。
// 调用时先放 to(20)，创建合约时没有这 20 字节。
// 后面是 value(32) + nonce(8) + gasLimit(8) + gasPrice(8) + chainId(8) + dataLen(4) + data。
// 没有 data 时，调用是 88 字节，创建是 68 字节。金额固定 32 字节，高位也签进去。
func (tx Tx) SigningBytes() []byte {
	buf := make([]byte, 0, 88+len(tx.Data))
	if !tx.Create {
		buf = append(buf, tx.To[:]...)
	}
	buf = append(buf, tx.Value[:]...)
	var num [8]byte
	binary.BigEndian.PutUint64(num[:], tx.Nonce)
	buf = append(buf, num[:]...)
	binary.BigEndian.PutUint64(num[:], tx.GasLimit)
	buf = append(buf, num[:]...)
	binary.BigEndian.PutUint64(num[:], tx.GasPrice)
	buf = append(buf, num[:]...)
	binary.BigEndian.PutUint64(num[:], tx.ChainID)
	buf = append(buf, num[:]...)
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(tx.Data)))
	buf = append(buf, n[:]...)
	buf = append(buf, tx.Data...)
	return buf
}

// TransactionsRoot 是交易的 Merkle Patricia Trie 根。
// 键是这笔交易在本块中的序号，从 0 起，再做 RLP。值是这笔交易的 RLP 列表。空列表的根是空树根。
func TransactionsRoot(txs []Tx) Hash {
	if len(txs) == 0 {
		return emptyTrieRoot()
	}
	keys := make([][]byte, len(txs))
	vals := make([][]byte, len(txs))
	for i, tx := range txs {
		keys[i] = rlpUint(uint64(i))
		vals[i] = tx.rlp()
	}
	return trieRoot(keys, vals)
}

// rlp 是交易树里这条记录的值。
// 列表顺序是 nonce、gasPrice、gasLimit、to、value、data、chainId、sig。
// 创建合约时 to 是空字符串；调用零地址时 to 仍是 20 个 0。sig 固定取 65 字节。
func (tx Tx) rlp() []byte {
	var to []byte
	if !tx.Create {
		to = tx.To[:]
	}
	sig := make([]byte, 65)
	copy(sig, tx.Sig)
	return rlpList(
		rlpUint(tx.Nonce),
		rlpUint(tx.GasPrice),
		rlpUint(tx.GasLimit),
		rlpString(to),
		tx.Value.rlp(),
		rlpString(tx.Data),
		rlpUint(tx.ChainID),
		rlpString(sig),
	)
}

// Sender 从签名恢复发送方。签名原文里的字段被改掉以后，恢复出的地址不再是原签名者。
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

// Sign 签一笔没有 data 的转账。chainID 不是 ChainID 时这里仍然成功，准入和执行才会拒绝。
func Sign(key []byte, to Address, value, nonce, gasLimit, gasPrice, chainID uint64) (Tx, error) {
	return SignAmount(key, to, U256(value), nonce, gasLimit, gasPrice, chainID)
}

// SignAmount 签一笔没有 data 的转账，金额是完整的 256 位。
func SignAmount(key []byte, to Address, value Uint256, nonce, gasLimit, gasPrice, chainID uint64) (Tx, error) {
	return signTx(key, Tx{To: to, Value: value, Nonce: nonce, GasLimit: gasLimit, GasPrice: gasPrice, ChainID: chainID})
}

// SignCall 签一笔带 data 的调用。
func SignCall(key []byte, to Address, value, nonce, gasLimit, gasPrice, chainID uint64, data []byte) (Tx, error) {
	return signTx(key, Tx{To: to, Data: data, Value: U256(value), Nonce: nonce, GasLimit: gasLimit, GasPrice: gasPrice, ChainID: chainID})
}

// SignCreate 签一笔创建合约的交易。to 为空，data 是初始代码。
func SignCreate(key []byte, value, nonce, gasLimit, gasPrice, chainID uint64, data []byte) (Tx, error) {
	return signTx(key, Tx{Create: true, Data: data, Value: U256(value), Nonce: nonce, GasLimit: gasLimit, GasPrice: gasPrice, ChainID: chainID})
}

func signTx(key []byte, tx Tx) (Tx, error) {
	sum := keccak(tx.SigningBytes())
	s, err := sig.Sign(key, sum[:])
	if err != nil {
		return Tx{}, err
	}
	tx.Sig = s
	return tx, nil
}
