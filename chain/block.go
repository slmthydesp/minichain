package chain

import "encoding/binary"

type Block struct {
	Height     uint64
	ParentHash Hash
	Coinbase   Address
	GasLimit   uint64
	GasUsed    uint64
	Txs        []Tx
	StateRoot  Hash
}

func (b Block) Hash() Hash {
	buf := make([]byte, 0, 104+len(b.Txs)*125)
	buf = append(buf, b.ParentHash[:]...)
	buf = append(buf, b.StateRoot[:]...)
	buf = append(buf, b.Coinbase[:]...)
	var gas [8]byte
	binary.BigEndian.PutUint64(gas[:], b.GasLimit)
	buf = append(buf, gas[:]...)
	binary.BigEndian.PutUint64(gas[:], b.GasUsed)
	buf = append(buf, gas[:]...)
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b.Txs)))
	buf = append(buf, n[:]...)
	for _, tx := range b.Txs {
		buf = append(buf, tx.CanonicalBytes()...)
	}
	return keccak(buf)
}
