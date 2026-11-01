package chain

import "testing"

func TestEmptyTrieRoot(t *testing.T) {
	if emptyTrieRoot().Hex() != emptyTrieRootHex {
		t.Fatalf("empty root = %s", emptyTrieRoot().Hex())
	}
	if trieRoot(nil, nil) != emptyTrieRoot() {
		t.Fatal("empty key list differed from the empty trie")
	}
}

func TestSingleLeafMatchesCompactRLP(t *testing.T) {
	got := trieRoot([][]byte{{0x01}}, [][]byte{{0x02}})
	enc := []byte{0xc4, 0x82, 0x20, 0x01, 0x02}
	if got != keccak(enc) {
		t.Fatalf("root = %s", got.Hex())
	}
}

func TestTrieRootIgnoresInsertionOrder(t *testing.T) {
	forward := trieRoot([][]byte{{0x01}, {0x02}}, [][]byte{{0x0a}, {0x0b}})
	reverse := trieRoot([][]byte{{0x02}, {0x01}}, [][]byte{{0x0b}, {0x0a}})
	if forward != reverse || forward == emptyTrieRoot() {
		t.Fatalf("forward = %s reverse = %s", forward.Hex(), reverse.Hex())
	}
}

func TestTrieRootDistinguishesPrefixKeys(t *testing.T) {
	short := trieRoot([][]byte{{0x61}}, [][]byte{{0x01}})
	both := trieRoot([][]byte{{0x61}, {0x61, 0x62}}, [][]byte{{0x01}, {0x02}})
	if short == both {
		t.Fatal("a prefix key did not change the root")
	}
}
