package chain

import (
	"bytes"
	"testing"

	"github.com/slmthydesp/minichain/sig"
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
	if len(sign) != 68 {
		t.Fatalf("signing bytes len = %d", len(sign))
	}
	canon := tx.CanonicalBytes()
	if len(canon) != 133 {
		t.Fatalf("canonical len = %d", len(canon))
	}
	if !bytes.Equal(canon[:68], sign) || !bytes.Equal(canon[68:], tx.Sig) {
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

func TestParseAddressAndHashRejectBadInput(t *testing.T) {
	if _, err := ParseAddress("0xzz"); err == nil {
		t.Fatal("expected address decode error")
	}
	if _, err := ParseAddress("0x01"); err == nil {
		t.Fatal("expected address length error")
	}
	if _, err := ParseHash("0xzz"); err == nil {
		t.Fatal("expected hash decode error")
	}
	if _, err := ParseHash("0xab"); err == nil {
		t.Fatal("expected hash length error")
	}
}

func TestCanonicalBytesAreAlways133(t *testing.T) {
	var to Address
	to[19] = 1
	short := Tx{To: to, Value: 1, Nonce: 2, Sig: []byte{1, 2, 3}}
	long := Tx{To: to, Value: 1, Nonce: 2, Sig: bytes.Repeat([]byte{9}, 80)}
	for _, tx := range []Tx{short, long} {
		canon := tx.CanonicalBytes()
		if len(canon) != 133 {
			t.Fatalf("canonical len = %d", len(canon))
		}
		if !bytes.Equal(canon[:68], tx.SigningBytes()) {
			t.Fatal("canonical prefix is not the signing bytes")
		}
	}
	if !bytes.Equal(long.CanonicalBytes()[68:], long.Sig[:65]) {
		t.Fatal("canonical signature was not the first 65 bytes")
	}
}

func TestSignRecoversDemoAddress(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 1
	tx, err := Sign(key, to, 10, 3, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Sig) != 65 || len(tx.CanonicalBytes()) != 133 {
		t.Fatalf("sig len = %d canonical len = %d", len(tx.Sig), len(tx.CanonicalBytes()))
	}
	got, err := tx.Sender()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	var want Address
	copy(want[:], raw[:])
	if got != want || tx.Value != 10 || tx.Nonce != 3 || tx.To != to {
		t.Fatalf("signed tx = %+v sender = %s", tx, got.Hex())
	}
	if _, err := Sign([]byte{1}, to, 1, 0, 21000, 1, ChainID); err == nil {
		t.Fatal("expected sign error for a short key")
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
	coinbase := base
	coinbase.Coinbase[19] = 1
	if base.Hash() == parent.Hash() || base.Hash() == root.Hash() || base.Hash() == txs.Hash() || base.Hash() == coinbase.Hash() {
		t.Fatal("hash did not change when parent, state root, transaction, or coinbase changed")
	}
}
