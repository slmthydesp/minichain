package chain

import (
	"errors"
	"testing"
)

func coverMachine(gas uint64) *machine {
	steps := maxSteps
	st := NewState()
	var contract Address
	contract[19] = 1
	st.SetAccount(Account{Address: contract, Balance: U256(1000)})
	return &machine{state: st, contract: contract, gas: gas, steps: &steps, depth: 1}
}

func pushHash(m *machine, h Hash) { m.stack = append(m.stack, h) }

func pushU64(m *machine, ns ...uint64) {
	for _, n := range ns {
		pushHash(m, hashFromUint64(n))
	}
}

func TestOpcodeStackFailures(t *testing.T) {
	ops := []func(*machine) error{
		func(m *machine) error { return m.opBalance() },
		func(m *machine) error { return m.opExtCodeSize() },
		func(m *machine) error { return m.opExtCodeHash() },
		func(m *machine) error { return m.opBlobHash() },
		func(m *machine) error { return m.opPop() },
		func(m *machine) error { return m.opDup(1) },
		func(m *machine) error { return m.opSwap(1) },
		func(m *machine) error { return m.opTload() },
		func(m *machine) error { return m.opTstore() },
		func(m *machine) error { return m.opKeccak() },
		func(m *machine) error { return m.opSelfdestruct() },
	}
	for i, op := range ops {
		m := coverMachine(100000)
		if err := op(m); !errors.Is(err, errHalt) {
			t.Fatalf("op %d err %v", i, err)
		}
	}
	m := coverMachine(100000)
	m.state.readOnly = true
	if err := m.opTstore(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	if err := m.opSelfdestruct(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	if err := m.opCreate(false); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushU64(m, 1)
	if err := m.opTstore(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushU64(m, 1)
	if err := m.opKeccak(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushHash(m, Hash{0: 1})
	if err := m.opBlockHash(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
}

func TestMemoryAndCopyEdges(t *testing.T) {
	m := coverMachine(100000)
	pushU64(m, 0xff, 0)
	if err := m.opMstore8(); err != nil || m.mem[0] != 0xff {
		t.Fatalf("mstore8 %v %x", err, m.mem)
	}
	m = coverMachine(100000)
	pushU64(m, 1)
	pushHash(m, Hash{0: 1})
	if err := m.opMstore8(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushU64(m, 1, maxMemorySize)
	if err := m.opMstore8(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}

	m = coverMachine(100000)
	pushU64(m, 0, 0, 0)
	if err := m.opMcopy(); err != nil {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushHash(m, Hash{0: 1})
	pushU64(m, 0, 0)
	if err := m.opMcopy(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushU64(m, 1, 0, maxMemorySize)
	if err := m.opMcopy(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushU64(m, 1, maxMemorySize, 0)
	if err := m.opMcopy(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	m.mem = []byte{9, 8, 7}
	pushU64(m, 1, 1, 0)
	if err := m.opMcopy(); err != nil || m.mem[0] != 8 {
		t.Fatalf("mcopy %v %x", err, m.mem)
	}

	m = coverMachine(100000)
	m.input = []byte{1, 2, 3, 4}
	pushU64(m, 2, 1, 0)
	if err := m.opCallDataCopy(); err != nil || m.mem[0] != 2 || m.mem[1] != 3 {
		t.Fatalf("calldatacopy %v %x", err, m.mem)
	}
	m = coverMachine(100000)
	pushHash(m, Hash{0: 1})
	pushU64(m, 0, 0)
	if err := m.opCallDataCopy(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}

	var other Address
	other[19] = 4
	m = coverMachine(100000)
	m.state.SetCode(other, []byte{5, 6, 7})
	pushU64(m, 2, 1, 0)
	pushHash(m, hashFromAddress(other))
	if err := m.opExtCodeCopy(); err != nil || m.mem[0] != 6 || m.mem[1] != 7 {
		t.Fatalf("extcodecopy %v %x", err, m.mem)
	}
	m = coverMachine(100000)
	pushU64(m, 0)
	if err := m.opExtCodeCopy(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushHash(m, Hash{0: 1})
	pushU64(m, 0, 0)
	pushHash(m, hashFromAddress(other))
	if err := m.opExtCodeCopy(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}

	m = coverMachine(100000)
	m.returnData = []byte{1, 2, 3}
	pushU64(m, 2, 0, 0)
	if err := m.opReturnDataCopy(); err != nil || m.mem[0] != 1 || m.mem[1] != 2 {
		t.Fatalf("returndatacopy %v %x", err, m.mem)
	}
	m = coverMachine(100000)
	m.returnData = []byte{1, 2, 3}
	pushU64(m, 2, 2, 0)
	if err := m.opReturnDataCopy(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushHash(m, Hash{0: 1})
	pushU64(m, 0, 0)
	if err := m.opReturnDataCopy(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}

	got := paddedSlice([]byte{1, 2}, ^uint64(0), 2)
	if got[0] != 0 || got[1] != 0 {
		t.Fatalf("padded %x", got)
	}
	m = coverMachine(1)
	if err := m.copyToMem(maxMemorySize, []byte{1}); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
}

func TestKeccakAndTransient(t *testing.T) {
	m := coverMachine(100000)
	pushU64(m, 0, 0)
	if err := m.opKeccak(); err != nil || m.stack[0] != keccak(nil) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushU64(m, ^uint64(0), 0)
	if err := m.opKeccak(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	pushHash(m, Hash{0: 1})
	pushU64(m, 0)
	if err := m.opKeccak(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(1)
	pushU64(m, 1, maxMemorySize)
	if err := m.opKeccak(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(100000)
	m.mem = []byte{'a'}
	pushU64(m, 1, 0)
	if err := m.opKeccak(); err != nil || m.stack[0] != keccak([]byte{'a'}) {
		t.Fatal(err)
	}

	m = coverMachine(100000)
	key := hashFromUint64(7)
	pushHash(m, hashFromUint64(9))
	pushHash(m, key)
	if err := m.opTstore(); err != nil {
		t.Fatal(err)
	}
	m.stack = nil
	pushHash(m, key)
	if err := m.opTload(); err != nil || m.stack[0] != hashFromUint64(9) {
		t.Fatalf("tload %v %x", err, m.stack)
	}
}

func TestCallFailureEdges(t *testing.T) {
	m := coverMachine(0)
	m.depth = maxDepth
	if err := m.opCall(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}

	m = coverMachine(100000)
	m.state.readOnly = true
	pushU64(m, 0, 0, 0, 0)
	pushU64(m, 1)
	pushHash(m, hashFromAddress(Address{19: 2}))
	pushU64(m, 0)
	if err := m.opCall(); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}

	m = coverMachine(100000)
	m.mem = []byte{0xab}
	pushU64(m, 0, 0, 1, 0)
	pushU64(m, 0)
	pushHash(m, hashFromAddress(Address{19: 2}))
	pushU64(m, 0)
	if err := m.opCall(); err != nil || m.stack[len(m.stack)-1] != hashFromUint64(1) {
		t.Fatalf("empty code call %v", err)
	}
}

func TestCreateEdges(t *testing.T) {
	if !deployable(nil, 0) || deployable([]byte{0xef}, 1000) || deployable(make([]byte, maxCodeSize+1), 0) {
		t.Fatal("deployable")
	}
	if deployable([]byte{1}, 0) {
		t.Fatal("deploy gas")
	}
	st := NewState()
	var addr Address
	addr[19] = 3
	if accountExists(st, addr) {
		t.Fatal("missing")
	}
	st.SetAccount(Account{Address: addr, Nonce: 1})
	if !accountExists(st, addr) {
		t.Fatal("nonce")
	}
	var coded Address
	coded[19] = 4
	st.SetCode(coded, []byte{1})
	if !accountExists(st, coded) {
		t.Fatal("code")
	}

	m := coverMachine(1)
	if err := m.opCreate(false); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(1000000)
	pushHash(m, Hash{0: 1})
	pushU64(m, 0)
	pushU64(m, 0)
	if err := m.opCreate(false); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(1000000)
	pushU64(m, maxInitCode+1, 0, 0)
	if err := m.opCreate(false); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(1000000)
	m.depth = maxDepth
	pushU64(m, 0, 0, 0)
	if err := m.opCreate(false); err != nil || m.stack[len(m.stack)-1] != (Hash{}) {
		t.Fatal(err)
	}
	m = coverMachine(1000000)
	pushU64(m, 1, maxMemorySize, 0)
	if err := m.opCreate(false); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}
	m = coverMachine(1)
	pushU64(m, 0, 0, 0)
	if err := m.opCreate(false); !errors.Is(err, errHalt) {
		t.Fatal(err)
	}

	m = coverMachine(1000000)
	m.state.SetAccount(Account{Address: m.contract, Balance: U256(0), Nonce: ^uint64(0)})
	pushU64(m, 0, 0, 0)
	if err := m.opCreate(false); err != nil || m.stack[len(m.stack)-1] != (Hash{}) {
		t.Fatal(err)
	}

	m = coverMachine(1000000)
	m.state.SetAccount(Account{Address: m.contract, Balance: U256(0)})
	pushU64(m, 0, 0, 1)
	if err := m.opCreate(false); err != nil || m.stack[len(m.stack)-1] != (Hash{}) {
		t.Fatal(err)
	}

	createdAt := CreateAddress(Address{19: 1}, 0)
	m = coverMachine(1000000)
	m.state.SetAccount(Account{Address: createdAt, Nonce: 1})
	pushU64(m, 0, 0, 0)
	if err := m.opCreate(false); err != nil || m.stack[len(m.stack)-1] != (Hash{}) {
		t.Fatal(err)
	}

	m = coverMachine(1000000)
	m.state.SetAccount(Account{Address: createdAt, Balance: MaxUint256()})
	pushU64(m, 0, 0, 1)
	if err := m.opCreate(false); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("overflow %v", err)
	}

	init := []byte{0x60, 0x00, 0x60, 0x00, 0xfd}
	m = coverMachine(1000000)
	m.mem = append([]byte(nil), init...)
	pushU64(m, uint64(len(init)), 0, 0)
	if err := m.opCreate(false); err != nil || m.stack[len(m.stack)-1] != (Hash{}) {
		t.Fatal(err)
	}

	bad := []byte{0xfe}
	m = coverMachine(1000000)
	m.mem = append([]byte(nil), bad...)
	pushU64(m, uint64(len(bad)), 0, 0)
	if err := m.opCreate(false); err != nil || m.stack[len(m.stack)-1] != (Hash{}) {
		t.Fatal(err)
	}

	ef := []byte{0x60, 0xef, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0xf3}
	m = coverMachine(1000000)
	m.mem = append([]byte(nil), ef...)
	pushU64(m, uint64(len(ef)), 0, 0)
	if err := m.opCreate(false); err != nil || m.stack[len(m.stack)-1] != (Hash{}) {
		t.Fatal(err)
	}

	m = coverMachine(1000000)
	pushHash(m, hashFromUint64(4))
	pushU64(m, 0, 0, 0)
	if err := m.opCreate(true); err != nil || m.stack[len(m.stack)-1] == (Hash{}) {
		t.Fatalf("create2 %v %x", err, m.stack)
	}
	m = coverMachine(1000000)
	pushU64(m, 0, 0, 0)
	if err := m.opCreate(false); err != nil || m.stack[len(m.stack)-1] == (Hash{}) {
		t.Fatalf("create %v %x", err, m.stack)
	}
}

func TestSelfdestructEdges(t *testing.T) {
	var other Address
	other[19] = 8

	m := coverMachine(100000)
	m.state.markCreated(m.contract)
	m.state.SetAccount(Account{Address: m.contract, Balance: U256(5)})
	m.state.SetAccount(Account{Address: other, Balance: MaxUint256()})
	pushHash(m, hashFromAddress(other))
	if err := m.opSelfdestruct(); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("overflow %v", err)
	}

	m = coverMachine(100000)
	m.state.markCreated(m.contract)
	m.state.SetAccount(Account{Address: m.contract, Balance: U256(5)})
	pushHash(m, hashFromAddress(m.contract))
	if err := m.opSelfdestruct(); !errors.Is(err, errStop) {
		t.Fatal(err)
	}
	bal, _ := m.state.GetBalanceNonce(m.contract)
	if !bal.IsZero() || !m.state.destructed[m.contract] {
		t.Fatalf("burned %s", bal)
	}

	m = coverMachine(100000)
	m.state.markCreated(m.contract)
	m.state.SetAccount(Account{Address: m.contract, Balance: U256(0)})
	pushHash(m, hashFromAddress(m.contract))
	if err := m.opSelfdestruct(); !errors.Is(err, errStop) {
		t.Fatal(err)
	}

	m = coverMachine(100000)
	m.state.SetAccount(Account{Address: m.contract, Balance: U256(4)})
	pushHash(m, hashFromAddress(other))
	if err := m.opSelfdestruct(); !errors.Is(err, errStop) {
		t.Fatal(err)
	}
	got, _ := m.state.GetBalanceNonce(other)
	if got != U256(4) {
		t.Fatalf("sent %s", got)
	}

	m = coverMachine(100000)
	pushHash(m, hashFromAddress(m.contract))
	if err := m.opSelfdestruct(); !errors.Is(err, errStop) {
		t.Fatal(err)
	}
}
