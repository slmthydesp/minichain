package chain

// 十六叉 Merkle Patricia Trie。键先拆成半字节（0 到 15），末尾再加一个终止符 16，表示这里是一条完整的键。
// 树上只有三种节点：
//   - 叶子或扩展是 shortNode：一段共同前缀后面只接一个孩子。孩子是值时它是叶子，是另一个节点时它是扩展。
//   - 分支是 fullNode：16 个半字节槽，外加第 17 槽放「键到此结束」的值。
//   - 值是 trieValue：调用方已经编码好的原始字节，不再往下分叉。
// 节点用 RLP 编码。小于 32 字节的节点直接嵌进父节点，否则父节点只留它的 Keccak-256。
// 根无论多短都再哈希一次。空树没有节点，根是 Keccak-256(0x80)。

// keccak256 "0x80"
const emptyTrieRootHex = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"

func emptyTrieRoot() Hash {
	return keccak([]byte{0x80})
}

// trieValue 是一条键对应的值。它不再有孩子。
type trieValue []byte

type trieNode interface{ isTrieNode() }

// shortNode 用一段半字节前缀收束唯一的后继。key 含终止符 16 时，val 必须是值，这是叶子。
// key 不含终止符时，val 是 fullNode 或另一段 shortNode，这是扩展。
type shortNode struct {
	key []byte
	val trieNode
}

// fullNode 按下一个半字节分成 16 岔。children[16] 不是孩子，而是恰好在这个节点结束的键的值。
type fullNode struct {
	children [17]trieNode
}

func (shortNode) isTrieNode() {}
func (fullNode) isTrieNode()  {}
func (trieValue) isTrieNode() {}

// trieRoot 计算一组键值的 Merkle Patricia Trie 根。键和值都是原始字节。
func trieRoot(keys, vals [][]byte) Hash {
	if len(keys) == 0 {
		return emptyTrieRoot()
	}
	var root trieNode
	for i, key := range keys {
		root = insertTrie(root, keybytesToHex(key), trieValue(vals[i]))
	}
	raw := hashTrie(root, true)
	var out Hash
	copy(out[:], raw)
	return out
}

// TransactionsRoot 是交易的 Merkle Patricia Trie 根。
// 键是序号的 RLP，和以太坊一样。值是这笔交易的规范编码。
func TransactionsRoot(txs []Tx) Hash {
	if len(txs) == 0 {
		return emptyTrieRoot()
	}
	keys := make([][]byte, len(txs))
	vals := make([][]byte, len(txs))
	for i, tx := range txs {
		keys[i] = rlpUint(uint64(i))
		vals[i] = tx.CanonicalBytes()
	}
	return trieRoot(keys, vals)
}

// insertTrie 把一条半字节键插进已有节点。两条键共享一段前缀时，共享部分留成 shortNode，分叉处改成 fullNode。
func insertTrie(n trieNode, key []byte, value trieNode) trieNode {
	if len(key) == 0 {
		return value
	}
	switch n := n.(type) {
	case nil:
		return &shortNode{key: append([]byte(nil), key...), val: value}
	case *shortNode:
		match := prefixLen(key, n.key)
		if match == len(n.key) {
			n.val = insertTrie(n.val, key[match:], value)
			return n
		}
		branch := &fullNode{}
		branch.children[n.key[match]] = insertTrie(nil, n.key[match+1:], n.val)
		branch.children[key[match]] = insertTrie(nil, key[match+1:], value)
		if match == 0 {
			return branch
		}
		return &shortNode{key: append([]byte(nil), key[:match]...), val: branch}
	case *fullNode:
		n.children[key[0]] = insertTrie(n.children[key[0]], key[1:], value)
		return n
	default:
		return value
	}
}

// hashTrie 先把节点编成 RLP。编码短于 32 字节且 force 为假时，原样返回，让父节点把它嵌进去。
// 否则返回 Keccak-256。根调用时 force 为真，所以根总是 32 字节哈希。
func hashTrie(n trieNode, force bool) []byte {
	var enc []byte
	switch n := n.(type) {
	case *shortNode:
		enc = encodeShort(n)
	case *fullNode:
		enc = encodeFull(n)
	default:
		panic("hashTrie on a value")
	}
	if len(enc) < 32 && !force {
		return enc
	}
	sum := keccak(enc)
	return sum[:]
}

func encodeShort(n *shortNode) []byte {
	key := rlpString(hexToCompact(n.key))
	if v, ok := n.val.(trieValue); ok {
		return rlpList(key, rlpString(v))
	}
	ref := hashTrie(n.val, false)
	if len(ref) < 32 {
		return rlpList(key, ref)
	}
	return rlpList(key, rlpString(ref))
}

func encodeFull(n *fullNode) []byte {
	items := make([][]byte, 17)
	for i := 0; i < 16; i++ {
		child := n.children[i]
		if child == nil {
			items[i] = []byte{0x80}
			continue
		}
		ref := hashTrie(child, false)
		if len(ref) < 32 {
			items[i] = ref
			continue
		}
		items[i] = rlpString(ref)
	}
	if v, ok := n.children[16].(trieValue); ok {
		items[16] = rlpString(v)
	} else {
		items[16] = []byte{0x80}
	}
	return rlpList(items...)
}

// keybytesToHex 把原始键拆成半字节，并在末尾加上终止符 16。终止符让「键是另一条键的前缀」仍能单独放一个值。
func keybytesToHex(str []byte) []byte {
	nibbles := make([]byte, len(str)*2+1)
	for i, b := range str {
		nibbles[i*2] = b / 16
		nibbles[i*2+1] = b % 16
	}
	nibbles[len(nibbles)-1] = 16
	return nibbles
}

// hexToCompact 把半字节路径压回写入节点的紧凑键。第一个字节的标志位记下奇数长度和是否带终止符。
func hexToCompact(hex []byte) []byte {
	terminator := byte(0)
	if hasTerm(hex) {
		terminator = 1
		hex = hex[:len(hex)-1]
	}
	buf := make([]byte, len(hex)/2+1)
	buf[0] = terminator << 5
	if len(hex)&1 == 1 {
		buf[0] |= 1 << 4
		buf[0] |= hex[0]
		hex = hex[1:]
	}
	for bi, ni := 0, 0; ni < len(hex); bi, ni = bi+1, ni+2 {
		buf[1+bi] = hex[ni]<<4 | hex[ni+1]
	}
	return buf
}

func hasTerm(s []byte) bool {
	return len(s) > 0 && s[len(s)-1] == 16
}

// prefixLen 是两条半字节路径从开头起连续相同的长度。插入时用它决定共享的 shortNode 有多长。
func prefixLen(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for ; i < n; i++ {
		if a[i] != b[i] {
			break
		}
	}
	return i
}
