package chain

import "testing"

// 这些测试核对 Osaka 操作码的栈结果。gas 仍是教学价，这里不跟以太坊的 gas 表比。

func withStore(code []byte) []byte {
	return append(append([]byte{}, code...), 0x60, 0x00, 0x55, 0x00)
}

func TestOsakaArithmeticAndBits(t *testing.T) {
	// 栈顶是左操作数。PUSH 的顺序是先压下一个，再压栈顶。
	tests := []struct {
		name string
		code []byte
		want Hash
	}{
		{"mul", []byte{0x60, 0x06, 0x60, 0x07, 0x02}, hashFromUint64(42)},
		{"div", []byte{0x60, 0x03, 0x60, 0x07, 0x04}, hashFromUint64(2)},
		{"div0", []byte{0x60, 0x00, 0x60, 0x01, 0x04}, Hash{}},
		// -2 / 2 = -1。先压除数 2，再算 0-2。
		{"sdiv", []byte{0x60, 0x02, 0x60, 0x02, 0x60, 0x00, 0x03, 0x05}, hashOnes()},
		{"mod", []byte{0x60, 0x03, 0x60, 0x08, 0x06}, hashFromUint64(2)},
		{"mod0", []byte{0x60, 0x00, 0x60, 0x01, 0x06}, Hash{}},
		// (2+3) % 4 = 1。栈上先压模数，再压加数。
		{"addmod", []byte{0x60, 0x04, 0x60, 0x03, 0x60, 0x02, 0x08}, hashFromUint64(1)},
		{"addmod0", []byte{0x60, 0x00, 0x60, 0x03, 0x60, 0x02, 0x08}, Hash{}},
		{"mulmod", []byte{0x60, 0x05, 0x60, 0x03, 0x60, 0x04, 0x09}, hashFromUint64(2)},
		// 2^3 = 8。栈顶是底数。
		{"exp", []byte{0x60, 0x03, 0x60, 0x02, 0x0a}, hashFromUint64(8)},
		// 0x80 的最低字节符号位为 1，扩展后高位全是 1。
		{"signextend", []byte{0x60, 0x80, 0x60, 0x00, 0x0b}, func() Hash {
			h := hashOnes()
			h[31] = 0x80
			return h
		}()},
		{"gt", []byte{0x60, 0x01, 0x60, 0x02, 0x11}, hashFromUint64(1)},
		{"not gt", []byte{0x60, 0x02, 0x60, 0x01, 0x11}, Hash{}},
		// -1 < 1，且 -1 不大于 1。
		{"slt", []byte{0x60, 0x01, 0x60, 0x01, 0x60, 0x00, 0x03, 0x12}, hashFromUint64(1)},
		{"sgt", []byte{0x60, 0x01, 0x60, 0x01, 0x60, 0x00, 0x03, 0x13}, Hash{}},
		{"and", []byte{0x60, 0xff, 0x60, 0x0f, 0x16}, hashFromUint64(0x0f)},
		{"or", []byte{0x60, 0x0f, 0x60, 0xf0, 0x17}, hashFromUint64(0xff)},
		{"xor", []byte{0x60, 0xff, 0x60, 0x0f, 0x18}, hashFromUint64(0xf0)},
		{"not", []byte{0x60, 0x00, 0x19}, hashOnes()},
		{"byte", []byte{0x60, 0xff, 0x60, 0x1f, 0x1a}, hashFromUint64(0xff)},
		{"byte oob", []byte{0x60, 0xff, 0x60, 0x20, 0x1a}, Hash{}},
		// 1 << 4 = 16。栈顶是移位数。
		{"shl", []byte{0x60, 0x01, 0x60, 0x04, 0x1b}, hashFromUint64(16)},
		{"shl wide", []byte{0x60, 0x01, 0x61, 0x01, 0x00, 0x1b}, Hash{}},
		{"shr", []byte{0x60, 0x10, 0x60, 0x01, 0x1c}, hashFromUint64(8)},
		// -1 算术右移 4 位仍是 -1。
		{"sar", []byte{0x60, 0x01, 0x60, 0x00, 0x03, 0x60, 0x04, 0x1d}, hashOnes()},
		{"clz1", []byte{0x60, 0x01, 0x1e}, hashFromUint64(255)},
		{"clz0", []byte{0x60, 0x00, 0x1e}, hashFromUint64(256)},
		{"push0", []byte{0x5f}, Hash{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, kind := runSlot0(t, withStore(tt.code))
			if kind != execOK {
				t.Fatalf("kind = %d", kind)
			}
			if got != tt.want {
				t.Fatalf("storage = %x\nwant     = %x", got, tt.want)
			}
		})
	}
}

func TestOsakaStackMemoryAndHash(t *testing.T) {
	dup, kind := runSlot0(t, withStore([]byte{0x60, 0x01, 0x60, 0x02, 0x81}))
	if kind != execOK || dup != hashFromUint64(1) {
		t.Fatalf("dup kind=%d got=%x", kind, dup)
	}
	sw, kind := runSlot0(t, withStore([]byte{0x60, 0x01, 0x60, 0x02, 0x90}))
	if kind != execOK || sw != hashFromUint64(1) {
		t.Fatalf("swap kind=%d got=%x", kind, sw)
	}
	popped, kind := runSlot0(t, withStore([]byte{0x60, 0x01, 0x60, 0x02, 0x50}))
	if kind != execOK || popped != hashFromUint64(1) {
		t.Fatalf("pop kind=%d got=%x", kind, popped)
	}

	// MSTORE8 只写最低字节。MLOAD 读回的字最高字节是 0xab。
	mem, kind := runSlot0(t, []byte{0x60, 0xab, 0x60, 0x00, 0x53, 0x60, 0x00, 0x51, 0x60, 0x00, 0x55, 0x00})
	if kind != execOK || mem[0] != 0xab {
		t.Fatalf("mstore8 kind=%d got=%x", kind, mem)
	}
	// 把偏移 0 的 1 字节拷到偏移 1。MLOAD 的前两个字节都是 0x11。
	copied, kind := runSlot0(t, []byte{
		0x60, 0x11, 0x60, 0x00, 0x53,
		0x60, 0x01, 0x60, 0x00, 0x60, 0x01, 0x5e,
		0x60, 0x00, 0x51, 0x60, 0x00, 0x55, 0x00,
	})
	if kind != execOK || copied[0] != 0x11 || copied[1] != 0x11 {
		t.Fatalf("mcopy kind=%d got=%x", kind, copied)
	}

	sum, kind := runSlot0(t, withStore([]byte{0x60, 0x00, 0x60, 0x00, 0x20}))
	if kind != execOK || sum != keccak(nil) {
		t.Fatalf("keccak kind=%d got=%x", kind, sum)
	}

	pc, kind := runSlot0(t, withStore([]byte{0x58}))
	if kind != execOK || pc != (Hash{}) {
		t.Fatalf("pc kind=%d got=%x", kind, pc)
	}
	msize, kind := runSlot0(t, []byte{0x60, 0x00, 0x60, 0x00, 0x52, 0x59, 0x60, 0x00, 0x55, 0x00})
	if kind != execOK || msize != hashFromUint64(32) {
		t.Fatalf("msize kind=%d got=%x", kind, msize)
	}
}

func TestOsakaContextOpcodes(t *testing.T) {
	st := NewState()
	var contract, rich, coin Address
	contract[19] = 1
	rich[19] = 2
	coin[19] = 3
	st.SetAccount(Account{Address: contract, Balance: 9})
	st.SetAccount(Account{Address: rich, Balance: 40})
	st.SetCoinbase(coin)
	st.SetBlock(8, 100)
	st.beginTx(rich, 7)
	parent := Hash{31: 0xaa}
	st.NoteBlockHash(7, parent)
	// BALANCE 需要栈上的地址。PUSH20 rich BALANCE SSTORE。
	balCode := append([]byte{0x73}, rich[:]...)
	balCode = append(balCode, 0x31, 0x60, 0x00, 0x55, 0x00)
	steps := maxSteps
	_, _, _, kind := runCode(st, contract, Address{}, 0, nil, balCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != hashFromUint64(40) {
		t.Fatalf("balance kind=%d got=%x", kind, st.GetStorage(contract, Hash{}))
	}

	selfCode := []byte{0x47, 0x60, 0x00, 0x55, 0x00}
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, selfCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != hashFromUint64(9) {
		t.Fatalf("selfbalance got=%x", st.GetStorage(contract, Hash{}))
	}
	originCode := []byte{0x32, 0x60, 0x00, 0x55, 0x00}
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, originCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != hashFromAddress(rich) {
		t.Fatalf("origin got=%x", st.GetStorage(contract, Hash{}))
	}
	priceCode := []byte{0x3a, 0x60, 0x00, 0x55, 0x00}
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, priceCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != hashFromUint64(7) {
		t.Fatalf("gasprice got=%x", st.GetStorage(contract, Hash{}))
	}
	coinCode := []byte{0x41, 0x60, 0x00, 0x55, 0x00}
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, coinCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != hashFromAddress(coin) {
		t.Fatalf("coinbase got=%x", st.GetStorage(contract, Hash{}))
	}
	idCode := []byte{0x46, 0x60, 0x00, 0x55, 0x00}
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, idCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != hashFromUint64(ChainID) {
		t.Fatalf("chainid got=%x", st.GetStorage(contract, Hash{}))
	}
	limitCode := []byte{0x45, 0x60, 0x00, 0x55, 0x00}
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, limitCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != hashFromUint64(BlockGasLimit) {
		t.Fatalf("gaslimit got=%x", st.GetStorage(contract, Hash{}))
	}
	hashCode := []byte{0x60, 0x07, 0x40, 0x60, 0x00, 0x55, 0x00}
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, hashCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != parent {
		t.Fatalf("blockhash got=%x", st.GetStorage(contract, Hash{}))
	}
	// 当前块和 256 块以前都读不到。
	missCode := []byte{0x60, 0x08, 0x40, 0x60, 0x00, 0x55, 0x00}
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, missCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != (Hash{}) {
		t.Fatalf("current blockhash got=%x", st.GetStorage(contract, Hash{}))
	}
	// 没有随机数、base fee、blob。
	for _, op := range []byte{0x44, 0x48, 0x4a} {
		zeroCode := []byte{op, 0x60, 0x00, 0x55, 0x00}
		_, _, _, kind = runCode(st, contract, Address{}, 0, nil, zeroCode, 100000, 1, &steps)
		if kind != execOK || st.GetStorage(contract, Hash{}) != (Hash{}) {
			t.Fatalf("op %x got=%x", op, st.GetStorage(contract, Hash{}))
		}
	}
	blobCode := []byte{0x60, 0x00, 0x49, 0x60, 0x00, 0x55, 0x00}
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, blobCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != (Hash{}) {
		t.Fatalf("blobhash got=%x", st.GetStorage(contract, Hash{}))
	}

	// 账户不存在时 EXTCODEHASH 是 0。存在但没代码时是空哈希。
	var missing, eoa Address
	missing[19] = 9
	eoa[19] = 8
	st.SetAccount(Account{Address: eoa})
	st.SetCode(rich, []byte{0x00})
	hashMissing := append([]byte{0x73}, missing[:]...)
	hashMissing = append(hashMissing, 0x3f, 0x60, 0x00, 0x55, 0x00)
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, hashMissing, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != (Hash{}) {
		t.Fatalf("missing hash got=%x", st.GetStorage(contract, Hash{}))
	}
	hashEOA := append([]byte{0x73}, eoa[:]...)
	hashEOA = append(hashEOA, 0x3f, 0x60, 0x00, 0x55, 0x00)
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, hashEOA, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != keccak(nil) {
		t.Fatalf("empty hash got=%x", st.GetStorage(contract, Hash{}))
	}
	sizeCode := append([]byte{0x73}, rich[:]...)
	sizeCode = append(sizeCode, 0x3b, 0x60, 0x00, 0x55, 0x00)
	_, _, _, kind = runCode(st, contract, Address{}, 0, nil, sizeCode, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(contract, Hash{}) != hashFromUint64(1) {
		t.Fatalf("extcodesize got=%x", st.GetStorage(contract, Hash{}))
	}

	rootBefore := StateRoot(st)
	st.NoteBlockHash(1, Hash{1: 1})
	if StateRoot(st) != rootBefore {
		t.Fatal("block hash changed the state root")
	}
}

func TestOsakaCreateDelegateAndStatic(t *testing.T) {
	st := NewState()
	var factory Address
	factory[19] = 1
	st.SetAccount(Account{Address: factory, Balance: 100})
	// 空初始代码的 CREATE。地址由创建前的 nonce 0 算出。
	createCode := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0xf0, 0x60, 0x00, 0x55, 0x00}
	steps := maxSteps
	_, _, _, kind := runCode(st, factory, Address{}, 0, nil, createCode, 200000, 1, &steps)
	want := CreateAddress(factory, 0)
	if kind != execOK || st.GetStorage(factory, Hash{}) != hashFromAddress(want) {
		t.Fatalf("create kind=%d got=%x want=%x", kind, st.GetStorage(factory, Hash{}), hashFromAddress(want))
	}
	created, ok := st.GetAccount(want)
	if !ok || created.Nonce != 1 {
		t.Fatalf("created account %+v ok=%v", created, ok)
	}
	acct, _ := st.GetAccount(factory)
	if acct.Nonce != 1 {
		t.Fatalf("factory nonce = %d", acct.Nonce)
	}

	create2 := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0xf5, 0x60, 0x00, 0x55, 0x00}
	_, _, _, kind = runCode(st, factory, Address{}, 0, nil, create2, 200000, 1, &steps)
	want2 := CreateAddress2(factory, Hash{}, nil)
	if kind != execOK || st.GetStorage(factory, Hash{}) != hashFromAddress(want2) {
		t.Fatalf("create2 got=%x want=%x", st.GetStorage(factory, Hash{}), hashFromAddress(want2))
	}

	// 0xef 开头的部署代码要失败。创建方 nonce 仍然加 1，新地址上不留账户。
	// 非法代码会烧掉初始代码剩下的 gas，所以这里 CREATE 之后直接 STOP，不再写存储。
	badInit := []byte{0x60, 0xef, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0xf3}
	bad := []byte{
		0x60, byte(len(badInit)), 0x60, 0x00, 0x60, 0x00, 0x39,
		0x60, byte(len(badInit)), 0x60, 0x00, 0x60, 0x00, 0xf0,
		0x00,
	}
	bad[3] = byte(len(bad))
	bad = append(bad, badInit...)
	_, nonceBefore := st.GetBalanceNonce(factory)
	_, _, _, kind = runCode(st, factory, Address{}, 0, nil, bad, 200000, 1, &steps)
	_, nonceAfter := st.GetBalanceNonce(factory)
	rejected := CreateAddress(factory, nonceBefore)
	if _, ok := st.GetAccount(rejected); ok || len(st.GetCode(rejected)) != 0 || kind != execOK || nonceAfter != nonceBefore+1 {
		t.Fatalf("ef kind=%d nonce %d -> %d account=%v", kind, nonceBefore, nonceAfter, rejected)
	}

	var callee Address
	callee[19] = 4
	// 被委托的代码把 7 写进槽 0，并把 CALLER 写进槽 1。
	st.SetCode(callee, []byte{0x60, 0x07, 0x60, 0x00, 0x55, 0x33, 0x60, 0x01, 0x55, 0x00})
	var caller Address
	caller[19] = 5
	delegate := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x73}
	delegate = append(delegate, callee[:]...)
	delegate = append(delegate, 0x61, 0x4e, 0x20, 0xf4, 0x00)
	_, _, _, kind = runCode(st, caller, factory, 3, nil, delegate, 200000, 1, &steps)
	if kind != execOK {
		t.Fatalf("delegate kind=%d", kind)
	}
	if st.GetStorage(caller, Hash{}) != hashFromUint64(7) {
		t.Fatalf("delegate storage = %x", st.GetStorage(caller, Hash{}))
	}
	if st.GetStorage(caller, hashFromUint64(1)) != hashFromAddress(factory) {
		t.Fatalf("delegate caller = %x", st.GetStorage(caller, hashFromUint64(1)))
	}
	if st.GetStorage(callee, Hash{}) != (Hash{}) {
		t.Fatalf("callee storage changed %x", st.GetStorage(callee, Hash{}))
	}

	// 只读调用里的 SSTORE 让这次调用失败，外层继续把 9 写进自己的槽 2。
	static := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x73}
	static = append(static, callee[:]...)
	static = append(static, 0x61, 0x4e, 0x20, 0xfa, 0x60, 0x03, 0x55, 0x60, 0x09, 0x60, 0x02, 0x55, 0x00)
	_, _, _, kind = runCode(st, caller, factory, 0, nil, static, 200000, 1, &steps)
	if kind != execOK {
		t.Fatalf("static kind=%d", kind)
	}
	if st.GetStorage(caller, hashFromUint64(3)) != (Hash{}) {
		t.Fatalf("static result = %x", st.GetStorage(caller, hashFromUint64(3)))
	}
	if st.GetStorage(caller, hashFromUint64(2)) != hashFromUint64(9) {
		t.Fatalf("outer store = %x", st.GetStorage(caller, hashFromUint64(2)))
	}
}

func TestCallCodeUsesOwnStorageAndTransfers(t *testing.T) {
	st := NewState()
	var caller, callee Address
	caller[19] = 1
	callee[19] = 2
	st.SetAccount(Account{Address: caller, Balance: 50})
	// 对方代码读 CALLVALUE 写进槽 0。CALLCODE 下这个槽属于调用方，金额转到对方。
	st.SetCode(callee, []byte{0x34, 0x60, 0x00, 0x55, 0x00})
	code := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x05, 0x73}
	code = append(code, callee[:]...)
	code = append(code, 0x61, 0x4e, 0x20, 0xf2, 0x00)
	steps := maxSteps
	_, _, _, kind := runCode(st, caller, Address{}, 0, nil, code, 200000, 1, &steps)
	if kind != execOK {
		t.Fatalf("kind=%d", kind)
	}
	if st.GetStorage(caller, Hash{}) != hashFromUint64(5) {
		t.Fatalf("caller storage = %x", st.GetStorage(caller, Hash{}))
	}
	if st.GetStorage(callee, Hash{}) != (Hash{}) {
		t.Fatalf("callee storage = %x", st.GetStorage(callee, Hash{}))
	}
	callerBal, _ := st.GetBalanceNonce(caller)
	calleeBal, _ := st.GetBalanceNonce(callee)
	if callerBal != 45 || calleeBal != 5 {
		t.Fatalf("balances caller=%d callee=%d", callerBal, calleeBal)
	}
}

func TestOsakaTransientReturnAndSelfdestruct(t *testing.T) {
	st := NewState()
	var caller, callee Address
	caller[19] = 1
	callee[19] = 2
	st.SetAccount(Account{Address: caller, Balance: 50})
	// 内层把瞬时槽写成 9 再回退。回退数据是 1 字节。
	st.SetCode(callee, []byte{
		0x60, 0x09, 0x60, 0x00, 0x5d,
		0x60, 0x2a, 0x60, 0x00, 0x53,
		0x60, 0x01, 0x60, 0x00, 0xfd,
	})
	// 外层先写成 1，调用之后再读出来，应该仍是 1。返回数据长度是 1。
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x5d, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x73}
	code = append(code, callee[:]...)
	code = append(code, 0x61, 0x4e, 0x20, 0xf1, 0x50, 0x60, 0x00, 0x5c, 0x60, 0x00, 0x55, 0x3d, 0x60, 0x01, 0x55, 0x00)
	steps := maxSteps
	_, _, _, kind := runCode(st, caller, Address{}, 0, nil, code, 200000, 1, &steps)
	if kind != execOK {
		t.Fatalf("kind=%d", kind)
	}
	if st.GetStorage(caller, Hash{}) != hashFromUint64(1) {
		t.Fatalf("transient = %x", st.GetStorage(caller, Hash{}))
	}
	if st.GetStorage(caller, hashFromUint64(1)) != hashFromUint64(1) {
		t.Fatalf("returndatasize = %x", st.GetStorage(caller, hashFromUint64(1)))
	}

	// EOF 的 RJUMP 不是 Osaka 传统执行的操作码。
	_, _, _, kind = runCode(st, caller, Address{}, 0, nil, []byte{0xe0}, 100000, 1, &steps)
	if kind != execHalt {
		t.Fatalf("eof kind=%d", kind)
	}

	// 同一笔交易里创建并 SELFDESTRUCT，成功结束后账户消失，余额到受益人。
	txSt, from := funded(t, 1000000, 0)
	var beneficiary Address
	beneficiary[19] = 7
	init := append([]byte{0x73}, beneficiary[:]...)
	init = append(init, 0xff)
	key := demoKey(t)
	create, err := SignCreate(key, 5, 0, 90000, 1, ChainID, init)
	if err != nil {
		t.Fatal(err)
	}
	receipt := mustApply(t, txSt, create)
	if receipt.Status != 1 {
		t.Fatalf("receipt = %+v", receipt)
	}
	addr := CreateAddress(from, 0)
	if _, ok := txSt.GetAccount(addr); ok {
		t.Fatal("selfdestructed account still exists")
	}
	if len(txSt.GetCode(addr)) != 0 {
		t.Fatal("code left behind")
	}
	got, _ := txSt.GetBalanceNonce(beneficiary)
	if got != 5 {
		t.Fatalf("beneficiary balance = %d", got)
	}

	// 早先部署的合约 SELFDESTRUCT 只转走余额，代码还在。
	runtime := append([]byte{0x73}, beneficiary[:]...)
	runtime = append(runtime, 0xff)
	keep, err := SignCreate(key, 0, 1, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	if receipt := mustApply(t, txSt, keep); receipt.Status != 1 {
		t.Fatalf("keep create = %+v", receipt)
	}
	old := CreateAddress(from, 1)
	fund, err := Sign(key, old, 4, 2, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 这笔是转账。合约有代码，所以会执行 SELFDESTRUCT。
	receipt = mustApply(t, txSt, fund)
	if receipt.Status != 1 {
		t.Fatalf("destruct call = %+v", receipt)
	}
	if len(txSt.GetCode(old)) == 0 {
		t.Fatal("existing contract code was deleted")
	}
	bal, _ := txSt.GetBalanceNonce(beneficiary)
	if bal != 9 {
		t.Fatalf("beneficiary after existing destruct = %d", bal)
	}
}
