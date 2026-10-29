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
}
