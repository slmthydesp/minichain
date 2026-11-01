package chain

import "encoding/binary"

// Block 是一个区块。交易池空着也要出块，空块是合法的。
// Coinbase 收取本块交易的 gas 费用。GasLimit 固定为 BlockGasLimit。
// GasUsed 是实际消耗之和。Time 是区块时间，必须大于父块。
// 交易和回执的原文不进哈希，哈希里只有三棵 Merkle Patricia Trie 的根，以及高度和时间。
type Block struct {
	Height           uint64
	ParentHash       Hash
	Coinbase         Address
	GasLimit         uint64
	GasUsed          uint64
	Txs              []Tx
	StateRoot        Hash
	TransactionsRoot Hash
	ReceiptsRoot     Hash
	Receipts         []Receipt
	Time             uint64
}

// Hash 的原文按这个顺序拼接，整数大端：
// parent(32) + stateRoot(32) + transactionsRoot(32) + receiptsRoot(32) + coinbase(20)
// + gasLimit(8) + gasUsed(8) + height(8) + time(8)。
func (b Block) Hash() Hash {
	buf := make([]byte, 0, 180)
	buf = append(buf, b.ParentHash[:]...)
	buf = append(buf, b.StateRoot[:]...)
	buf = append(buf, b.TransactionsRoot[:]...)
	buf = append(buf, b.ReceiptsRoot[:]...)
	buf = append(buf, b.Coinbase[:]...)
	var num [8]byte
	binary.BigEndian.PutUint64(num[:], b.GasLimit)
	buf = append(buf, num[:]...)
	binary.BigEndian.PutUint64(num[:], b.GasUsed)
	buf = append(buf, num[:]...)
	binary.BigEndian.PutUint64(num[:], b.Height)
	buf = append(buf, num[:]...)
	binary.BigEndian.PutUint64(num[:], b.Time)
	buf = append(buf, num[:]...)
	return keccak(buf)
}
