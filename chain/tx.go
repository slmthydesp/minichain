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
