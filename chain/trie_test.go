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

// 新键包含已有 shortNode 的整段键。这段键留在原节点上，剩下的半字节继续往它的孩子里插。
// 孩子已经是值时，插入直接换成新值。
// 两条键只在最后一个字节不同，值都有 64 字节。共享前缀收成 shortNode，孩子编码超过 32 字节，父节点只留哈希。
func TestInsertExtendsShortNodeAndHashedChild(t *testing.T) {
	// 先放 [1,2,3]。再放更长的 [1,2,3,4,16]，整段旧键都对得上，返回的还是这个 shortNode。
	root := insertTrie(nil, []byte{1, 2, 3}, trieValue{0x01})
	root = insertTrie(root, []byte{1, 2, 3, 4, 16}, trieValue{0x02})
	if root == nil {
		t.Fatal("extended short node")
	}
	// 节点已经是值。再插一条键，结果就是新值 0x02。
	replaced := insertTrie(trieValue{0x01}, []byte{1}, trieValue{0x02})
	if v, ok := replaced.(trieValue); !ok || v[0] != 0x02 {
		t.Fatalf("value node = %v", replaced)
	}
	// k1 全 0，k2 只在最后一字节是 1。两条值各 64 字节，叶子装不下，shortNode 的孩子被哈希。算出来的根不是空树根。
	k1 := make([]byte, 32)
	k2 := make([]byte, 32)
	k2[31] = 1
	v1 := make([]byte, 64)
	v2 := make([]byte, 64)
	v1[0], v2[0] = 1, 2
	got := trieRoot([][]byte{k1, k2}, [][]byte{v1, v2})
	if got == emptyTrieRoot() {
		t.Fatal("hashed short child")
	}
}

// hashTrie 只编码 shortNode 和 fullNode。值节点没有自己的节点编码，传进去会 panic。
func TestHashTriePanicsOnValue(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	hashTrie(trieValue{0x01}, true)
}
