package sig

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestDemoKeyAddress(t *testing.T) {
	key, err := ParseKey(DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	got, err := KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString("f39fd6e51aad88f6f4ce6ab8827279cfffb92266")
	if !bytes.Equal(got[:], want) {
		t.Fatalf("address = %x", got)
	}
}

func TestVerifyRejectsFlippedSignatureByte(t *testing.T) {
	key, err := ParseKey(DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	hash := Keccak([]byte("minichain"))
	signature, err := Sign(key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(addr, hash[:], signature); err != nil {
		t.Fatal(err)
	}
	flipped := bytes.Clone(signature)
	flipped[10] ^= 0x01
	if err := Verify(addr, hash[:], flipped); err == nil {
		t.Fatal("expected flipped signature to fail")
	}
	var other [20]byte
	if err := Verify(other, hash[:], signature); err == nil {
		t.Fatal("expected address mismatch")
	}
}

func TestSigRejectsBadInputs(t *testing.T) {
	if _, err := ParseKey("zz"); err == nil {
		t.Fatal("expected bad hex")
	}
	if _, err := ParseKey("0xaa"); err == nil {
		t.Fatal("expected short key")
	}
	if _, err := KeyAddress([]byte{1}); err == nil {
		t.Fatal("expected short key")
	}
	key, err := ParseKey(DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Sign([]byte{1}, make([]byte, 32)); err == nil {
		t.Fatal("expected short key")
	}
	if _, err := Sign(key, []byte{1}); err == nil {
		t.Fatal("expected short hash")
	}
	if _, err := Recover(make([]byte, 32), []byte{1}); err == nil {
		t.Fatal("expected bad signature length")
	}
	if _, err := Recover(make([]byte, 32), make([]byte, 65)); err == nil {
		t.Fatal("expected recover failure")
	}
	if err := Verify([20]byte{}, make([]byte, 32), []byte{1}); err == nil {
		t.Fatal("expected verify failure")
	}
}
