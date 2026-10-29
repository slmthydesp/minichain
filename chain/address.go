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
