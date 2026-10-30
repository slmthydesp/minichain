package chain

import "encoding/binary"

type Block struct {
	Height     uint64
	ParentHash Hash
	Coinbase   Address
	Txs        []Tx
	StateRoot  Hash
}

func (b Block) Hash() Hash {
	buf := make([]byte, 0, 88+len(b.Txs)*101)
	buf = append(buf, b.ParentHash[:]...)
	buf = append(buf, b.StateRoot[:]...)
	buf = append(buf, b.Coinbase[:]...)
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b.Txs)))
	buf = append(buf, n[:]...)
	for _, tx := range b.Txs {
		buf = append(buf, tx.CanonicalBytes()...)
	}
	return keccak(buf)
}
