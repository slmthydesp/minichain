package chain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

func TestSigningBytesIncludeChainID(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 1
	tx, err := Sign(key, to, 10, 3, 21000, 1, 6666)
	if err != nil {
		t.Fatal(err)
	}
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
	var wantField [8]byte
	binary.BigEndian.PutUint64(wantField[:], 21000)
	if !bytes.Equal(sign[36:44], wantField[:]) {
		t.Fatalf("gas limit bytes = %x", sign[36:44])
	}
	binary.BigEndian.PutUint64(wantField[:], 1)
	if !bytes.Equal(sign[44:52], wantField[:]) {
		t.Fatalf("gas price bytes = %x", sign[44:52])
	}
	binary.BigEndian.PutUint64(wantField[:], 6666)
	if !bytes.Equal(sign[52:60], wantField[:]) {
		t.Fatalf("chain id bytes = %x", sign[52:60])
	}
	if !bytes.Equal(sign[60:], bytes.Repeat([]byte{0}, 8)) {
		t.Fatalf("signing tail = %x", sign[60:])
	}
	got, err := tx.Sender()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	var demo Address
	copy(demo[:], raw[:])
	if got != demo {
		t.Fatalf("sender = %s", got.Hex())
	}

	tx.ChainID = 6667
	recovered, err := tx.Sender()
	if err == nil && recovered == demo {
		t.Fatal("chain id change still recovered the signer")
	}
}

func TestPoolRejectsWrongChainID(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 1
	tx, err := Sign(key, to, 10, 0, 21000, 1, 6667)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Sender(); err != nil {
		t.Fatal(err)
	}
	if err := NewPool().Add(tx, 100, 0); !errors.Is(err, ErrWrongChainID) {
		t.Fatalf("pool err = %v", err)
	}
	if err := Apply(NewState(), tx); !errors.Is(err, ErrWrongChainID) {
		t.Fatalf("apply err = %v", err)
	}
}
