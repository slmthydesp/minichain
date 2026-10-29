package chain

import (
	"bytes"
	"testing"
)

func TestAccountFieldsRoundTrip(t *testing.T) {
	var addr Address
	addr[19] = 7
	got := Account{Address: addr, Balance: 42, Nonce: 3}
	if got.Address != addr || got.Balance != 42 || got.Nonce != 3 {
		t.Fatalf("account = %+v", got)
	}
}

func TestTxSigningAndCanonicalBytes(t *testing.T) {
	var to Address
	to[19] = 1
	tx := Tx{To: to, Value: 9, Nonce: 4, Sig: bytes.Repeat([]byte{0xab}, 65)}
	sign := tx.SigningBytes()
	if len(sign) != 36 {
		t.Fatalf("signing bytes len = %d", len(sign))
	}
	canon := tx.CanonicalBytes()
	if len(canon) != 101 {
		t.Fatalf("canonical len = %d", len(canon))
	}
	if !bytes.Equal(canon[:36], sign) || !bytes.Equal(canon[36:], tx.Sig) {
		t.Fatal("canonical bytes are not signing bytes plus signature")
	}
	if sign[19] != 1 || sign[27] != 9 || sign[35] != 4 {
		t.Fatalf("signing layout = %x", sign)
	}
}

func TestParseAddressAndHash(t *testing.T) {
	addr, err := ParseAddress("0xF39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	if err != nil {
		t.Fatal(err)
	}
	if addr.Hex() != "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266" {
		t.Fatalf("address hex = %s", addr.Hex())
	}
	const hexHash = "abababababababababababababababababababababababababababababababab"
	h, err := ParseHash("0x" + hexHash)
	if err != nil {
		t.Fatal(err)
	}
	if h.Hex() != "0x"+hexHash {
		t.Fatalf("hash hex = %s", h.Hex())
	}
}

func TestBlockHashChangesWhenContentsChange(t *testing.T) {
	var to Address
	to[19] = 1
	base := Block{
		Height:     1,
		ParentHash: Hash{1},
		StateRoot:  Hash{2},
		Txs: []Tx{{
			To: to, Value: 1, Nonce: 0,
			Sig: bytes.Repeat([]byte{3}, 65),
		}},
	}
	same := base
	if base.Hash() != same.Hash() {
		t.Fatal("same contents produced different hashes")
	}
	parent := base
	parent.ParentHash[0] ^= 0x01
	root := base
	root.StateRoot[0] ^= 0x01
	txs := base
	txs.Txs = []Tx{txs.Txs[0]}
	txs.Txs[0].Sig = append([]byte(nil), txs.Txs[0].Sig...)
	txs.Txs[0].Sig[0] ^= 0x01
	if base.Hash() == parent.Hash() || base.Hash() == root.Hash() || base.Hash() == txs.Hash() {
		t.Fatal("hash did not change when parent, state root, or transaction changed")
	}
}
