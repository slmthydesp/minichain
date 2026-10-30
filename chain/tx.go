package chain

import (
	"encoding/binary"

	"github.com/slmthydesp/minichain/sig"
)

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
	buf := make([]byte, 101)
	copy(buf, tx.SigningBytes())
	copy(buf[36:101], tx.Sig)
	return buf
}

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
