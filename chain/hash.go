package chain

import "golang.org/x/crypto/sha3"

func keccak(data []byte) Hash {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(data)
	sum := h.Sum(nil)
	var out Hash
	copy(out[:], sum)
	return out
}
