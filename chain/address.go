package chain

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// Address 是账户地址，固定 20 字节，由公钥的 Keccak-256 取后 20 字节得到。
type Address [20]byte

func (a Address) Hex() string {
	return "0x" + hex.EncodeToString(a[:])
}

// ParseAddress 解析十六进制地址，可以带 0x 前缀。解码后必须正好是 20 字节。
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

// Hash 是 32 字节的 Keccak-256 摘要，用作区块哈希、状态根和回执根。
type Hash [32]byte

func (h Hash) Hex() string {
	return "0x" + hex.EncodeToString(h[:])
}

// ParseHash 解析十六进制哈希，可以带 0x 前缀。解码后必须正好是 32 字节。
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
