// Package sig 提供本链使用的 Keccak-256 和 secp256k1 签名。
// 地址由未压缩公钥去掉前缀后做 Keccak，再取后 20 字节。签名是 65 字节的可恢复格式。
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

// DemoKeyHex 是公开的演示私钥，对应地址 0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266。创世余额记在这个账户上。
const DemoKeyHex = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// Keccak 计算以太坊使用的 Keccak-256，不是 NIST 的 SHA3-256。结果是 32 字节。
func Keccak(data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(data)
	sum := h.Sum(nil)
	var out [32]byte
	copy(out[:], sum)
	return out
}

// ParseKey 解析 32 字节私钥，可以带 0x 前缀。
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

// KeyAddress 由私钥推出地址。未压缩公钥是 0x04 加 64 字节坐标，地址只对这 64 字节做 Keccak，再取后 20 字节。
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

// Sign 对已经算好的 32 字节摘要签名，不再做一次哈希。返回 65 字节：恢复标记、R、S。
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

// Recover 用 65 字节签名和 32 字节摘要恢复出地址。摘要被改过时，恢复出的是另一把钥匙的地址。
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

// Verify 恢复签名里的地址，并要求它等于 addr。
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
