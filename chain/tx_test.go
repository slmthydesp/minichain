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
	// mock the to address
	var to Address
	to[19] = 1
	tx, err := Sign(key, to, 10, 3, 21000, 1, 6666)
	if err != nil {
		t.Fatal(err)
	}
	sign := tx.SigningBytes()
	if len(sign) != 88 {
		t.Fatalf("signing bytes len = %d", len(sign))
	}
	var wantValue Uint256
	binary.BigEndian.PutUint64(wantValue[24:], 10)
	if !bytes.Equal(sign[20:52], wantValue[:]) {
		t.Fatalf("value bytes = %x", sign[20:52])
	}
	var wantField [8]byte
	binary.BigEndian.PutUint64(wantField[:], 21000)
	if !bytes.Equal(sign[60:68], wantField[:]) {
		t.Fatalf("gas limit bytes = %x", sign[60:68])
	}
	binary.BigEndian.PutUint64(wantField[:], 1)
	if !bytes.Equal(sign[68:76], wantField[:]) {
		t.Fatalf("gas price bytes = %x", sign[68:76])
	}
	binary.BigEndian.PutUint64(wantField[:], 6666)
	if !bytes.Equal(sign[76:84], wantField[:]) {
		t.Fatalf("chain id bytes = %x", sign[76:84])
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
	recovered, _ := tx.Sender()
	if recovered == demo {
		t.Fatal("chain id change still recovered the signer")
	}
}

func TestCreateOmitsToAndZeroAddressIsACall(t *testing.T) {
	key := demoKey(t)
	call, err := Sign(key, Address{}, 10, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if call.Create {
		t.Fatal("zero address was signed as create")
	}
	sign := call.SigningBytes()
	if len(sign) != 88 || !bytes.Equal(sign[:20], make([]byte, 20)) {
		t.Fatalf("zero-address signing len = %d prefix = %x", len(sign), sign[:min(20, len(sign))])
	}
	create, err := SignCreate(key, 10, 0, 53000, 1, ChainID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !create.Create {
		t.Fatal("create flag was not set")
	}
	created := create.SigningBytes()
	if len(created) != 68 {
		t.Fatalf("create signing len = %d", len(created))
	}
	if bytes.Equal(sign, created) {
		t.Fatal("create and a call to the zero address signed the same bytes")
	}
}

func TestTransactionTrieValueIsRLPList(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 1
	tx, err := SignCall(key, to, 10, 3, 21000, 1, ChainID, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 65)
	copy(sig, tx.Sig)
	want := transactionRLP(tx, sig)
	if !bytes.Equal(tx.rlp(), want) {
		t.Fatalf("rlp = %x", tx.rlp())
	}
	if TransactionsRoot([]Tx{tx}) != trieRoot([][]byte{rlpUint(0)}, [][]byte{want}) {
		t.Fatal("transactions root did not use the rlp value")
	}

	create, err := SignCreate(key, 10, 0, 53000, 1, ChainID, nil)
	if err != nil {
		t.Fatal(err)
	}
	call, err := Sign(key, Address{}, 10, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(create.rlp(), call.rlp()) {
		t.Fatal("create and a call to the zero address encoded the same rlp")
	}
}

func TestTransactionRLPSignatureIs65Bytes(t *testing.T) {
	var to Address
	to[19] = 1
	short := Tx{To: to, Value: U256(1), Nonce: 2, Sig: []byte{1, 2, 3}}
	long := Tx{To: to, Value: U256(1), Nonce: 2, Sig: bytes.Repeat([]byte{9}, 80)}
	shortSig := make([]byte, 65)
	copy(shortSig, short.Sig)
	if !bytes.Equal(short.rlp(), transactionRLP(short, shortSig)) {
		t.Fatalf("short sig rlp = %x", short.rlp())
	}
	if !bytes.Equal(long.rlp(), transactionRLP(long, long.Sig[:65])) {
		t.Fatalf("long sig rlp = %x", long.rlp())
	}
}

func transactionRLP(tx Tx, sig []byte) []byte {
	return rlpList(
		rlpUint(tx.Nonce),
		rlpUint(tx.GasPrice),
		rlpUint(tx.GasLimit),
		rlpString(tx.To[:]),
		tx.Value.rlp(),
		rlpString(tx.Data),
		rlpUint(tx.ChainID),
		rlpString(sig),
	)
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
	if err := NewPool().AddTx(tx, U256(100), 0); !errors.Is(err, ErrWrongChainID) {
		t.Fatalf("pool err = %v", err)
	}
	if _, err := Apply(NewState(), tx); !errors.Is(err, ErrWrongChainID) {
		t.Fatalf("apply err = %v", err)
	}
}
