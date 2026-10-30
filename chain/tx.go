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

const ChainID uint64 = 6666

type Tx struct {
	To       Address
	Value    uint64
	Nonce    uint64
	GasLimit uint64
	GasPrice uint64
	ChainID  uint64
	Sig      []byte
}

func (tx Tx) SigningBytes() []byte {
	// Named fields occupy 60 bytes. The signed payload is 68, so the tail stays zero.
	buf := make([]byte, 68)
	copy(buf[:20], tx.To[:])
	binary.BigEndian.PutUint64(buf[20:28], tx.Value)
	binary.BigEndian.PutUint64(buf[28:36], tx.Nonce)
	binary.BigEndian.PutUint64(buf[36:44], tx.GasLimit)
	binary.BigEndian.PutUint64(buf[44:52], tx.GasPrice)
	binary.BigEndian.PutUint64(buf[52:60], tx.ChainID)
	return buf
}

func (tx Tx) CanonicalBytes() []byte {
	buf := make([]byte, 133)
	copy(buf, tx.SigningBytes())
	copy(buf[68:133], tx.Sig)
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

func Sign(key []byte, to Address, value, nonce, gasLimit, gasPrice, chainID uint64) (Tx, error) {
	tx := Tx{To: to, Value: value, Nonce: nonce, GasLimit: gasLimit, GasPrice: gasPrice, ChainID: chainID}
	sum := keccak(tx.SigningBytes())
	s, err := sig.Sign(key, sum[:])
	if err != nil {
		return Tx{}, err
	}
	tx.Sig = s
	return tx, nil
}
