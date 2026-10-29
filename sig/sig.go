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

const DemoKeyHex = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func Keccak(data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(data)
	sum := h.Sum(nil)
	var out [32]byte
	copy(out[:], sum)
	return out
}

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
