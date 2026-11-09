package chain

import (
	"errors"
	"testing"
)

func runVM(t *testing.T, code []byte, gas uint64, steps int, input []byte) ([]byte, []Log, uint64, execKind) {
	t.Helper()
	st := NewState()
	var addr Address
	addr[19] = 1
	return runCode(st, addr, Address{}, U256(0), input, code, gas, 1, &steps)
}

func TestRunCodeStopsWhenStepsAreExhausted(t *testing.T) {
	_, _, left, kind := runVM(t, []byte{0x00}, 100000, 0, nil)
	if kind != execHalt || left != 0 {
		t.Fatalf("kind=%d left=%d", kind, left)
	}
}

func TestRunCodeFallsOffEndWithoutReturn(t *testing.T) {
	ret, _, _, kind := runVM(t, []byte{0x5b}, 100000, maxSteps, nil)
	if kind != execOK || ret != nil {
		t.Fatalf("kind=%d ret=%v", kind, ret)
	}
}

func TestCalldataSizeAndUnknownOpcode(t *testing.T) {
	st := NewState()
	var addr Address
	addr[19] = 1
	code := []byte{0x36, 0x60, 0x00, 0x55, 0x00}
	steps := maxSteps
	_, _, _, kind := runCode(st, addr, Address{}, U256(0), []byte{1, 2, 3}, code, 100000, 1, &steps)
	if kind != execOK || st.GetStorage(addr, Hash{}) != hashFromUint64(3) {
		t.Fatalf("kind=%d size=%x", kind, st.GetStorage(addr, Hash{}))
	}
	_, _, _, kind = runVM(t, []byte{0xfe}, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("unknown opcode kind=%d", kind)
	}
}

func TestChargeAndStackUnderflowHalt(t *testing.T) {
	tests := []struct {
		name string
		code []byte
		gas  uint64
	}{
		{"add gas", []byte{0x60, 0x01, 0x60, 0x01, 0x01}, 5},
		{"iszero", []byte{0x15}, 100000},
		{"add", []byte{0x01}, 100000},
		{"calldataload", []byte{0x35}, 100000},
		{"codecopy", []byte{0x39}, 100000},
		{"mload", []byte{0x51}, 100000},
		{"mstore", []byte{0x52}, 100000},
		{"log0", []byte{0xa0}, 100000},
		{"return", []byte{0xf3}, 100000},
		{"call", []byte{0xf1}, 100000},
		{"log1 topics", []byte{0x60, 0x00, 0x60, 0x00, 0xa1}, 100000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, kind := runVM(t, tt.code, tt.gas, maxSteps, nil)
			if kind != execHalt {
				t.Fatalf("kind=%d", kind)
			}
		})
	}
}

func TestPushTruncatesImmediate(t *testing.T) {
	_, _, _, kind := runVM(t, []byte{0x61}, 100000, maxSteps, nil)
	if kind != execOK {
		t.Fatalf("empty immediate kind=%d", kind)
	}
	_, _, _, kind = runVM(t, []byte{0x61, 0x01}, 100000, maxSteps, nil)
	if kind != execOK {
		t.Fatalf("short immediate kind=%d", kind)
	}
}

func TestMemoryAndLogRejectOversizedRange(t *testing.T) {
	// 0x040000 对齐后约 8193 个字，二次费用超过这里给的 100000 gas。
	overMem := []byte{0x62, 0x04, 0x00, 0x00, 0x51}
	_, _, _, kind := runVM(t, overMem, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("mload kind=%d", kind)
	}
	size := hashFromUint64(^uint64(0)/gasLogData + 1)
	logOverflow := append(append([]byte{0x7f}, size[:]...), 0x60, 0x00, 0xa0)
	_, _, _, kind = runVM(t, logOverflow, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("log overflow kind=%d", kind)
	}
	logGas := []byte{0x60, 0x01, 0x60, 0x00, 0xa0}
	_, _, _, kind = runVM(t, logGas, 10, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("log gas kind=%d", kind)
	}
	logGrow := []byte{0x60, 0x02, 0x62, 0x04, 0x00, 0x00, 0xa0}
	_, _, _, kind = runVM(t, logGrow, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("log grow kind=%d", kind)
	}
	retGrow := []byte{0x60, 0x01, 0x62, 0x04, 0x00, 0x00, 0xf3}
	_, _, _, kind = runVM(t, retGrow, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("return grow kind=%d", kind)
	}
	copyGrow := []byte{0x60, 0x01, 0x60, 0x00, 0x62, 0x04, 0x00, 0x00, 0x39}
	_, _, _, kind = runVM(t, copyGrow, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("codecopy grow kind=%d", kind)
	}
	mstoreGrow := []byte{0x60, 0x00, 0x62, 0x04, 0x00, 0x00, 0x52}
	_, _, _, kind = runVM(t, mstoreGrow, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("mstore grow kind=%d", kind)
	}
}

func TestGrowRejectsWhenMemoryGasIsInsufficient(t *testing.T) {
	code := []byte{0x60, 0x00, 0x60, 0x20, 0x51, 0x00}
	_, _, _, kind := runVM(t, code, gasBase+gasBase+1, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("kind=%d", kind)
	}
}

func TestStackOverflowAndWideUint(t *testing.T) {
	code := make([]byte, 0, 2*(maxStack+1))
	for i := 0; i < maxStack+1; i++ {
		code = append(code, 0x60, 0x00)
	}
	_, _, _, kind := runVM(t, code, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("stack kind=%d", kind)
	}
	wide := Hash{0: 1}
	jumpWide := append(append([]byte{0x7f}, wide[:]...), 0x56)
	_, _, _, kind = runVM(t, jumpWide, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("wide jump kind=%d", kind)
	}
}

func TestJumpFalseAndEqualLt(t *testing.T) {
	m := &machine{}
	if err := m.jump(false); err != nil {
		t.Fatal(err)
	}
	got, kind := runSlot0(t, codeJumpIf([]byte{0x60, 0x01, 0x60, 0x01, 0x10}))
	if kind != execOK || got != (Hash{}) {
		t.Fatalf("equal lt kind=%d storage=%x", kind, got)
	}
}

func callStack(to Address, gas, value, inOff, inSize, retOff, retSize byte) []byte {
	code := []byte{0x60, retSize, 0x60, retOff, 0x60, inSize, 0x60, inOff, 0x60, value, 0x73}
	code = append(code, to[:]...)
	return append(code, 0x60, gas, 0xf1, 0x00)
}

func TestCallEmptyCodeAndInnerResults(t *testing.T) {
	st := NewState()
	var contract, to Address
	contract[19] = 1
	to[19] = 2
	st.SetAccount(Account{Address: contract, Balance: U256(50)})
	steps := maxSteps
	code := callStack(to, 50, 0, 0, 0, 0, 0)
	_, _, _, kind := runCode(st, contract, Address{}, U256(0), nil, code, 100000, 1, &steps)
	if kind != execOK {
		t.Fatalf("empty code kind=%d", kind)
	}

	st.SetCode(to, []byte{0x60, 0x00, 0x60, 0x00, 0xfd})
	_, _, _, kind = runCode(st, contract, Address{}, U256(0), nil, code, 100000, 1, &steps)
	if kind != execOK {
		t.Fatalf("inner revert kind=%d", kind)
	}

	st.SetCode(to, []byte{0xfe})
	_, _, _, kind = runCode(st, contract, Address{}, U256(0), nil, code, 100000, 1, &steps)
	if kind != execOK {
		t.Fatalf("inner halt kind=%d", kind)
	}

	st.SetCode(to, []byte{0x60, 0x01, 0x60, 0x00, 0xf3})
	retCall := callStack(to, 50, 0, 0, 0, 0, 32)
	_, _, _, kind = runCode(st, contract, Address{}, U256(0), nil, retCall, 100000, 1, &steps)
	if kind != execOK {
		t.Fatalf("short return kind=%d", kind)
	}
}

func TestCallValueFailures(t *testing.T) {
	st := NewState()
	var contract, to Address
	contract[19] = 1
	to[19] = 2
	code := callStack(to, 50, 2, 0, 0, 0, 0)
	steps := maxSteps
	_, _, _, kind := runCode(st, contract, Address{}, U256(0), nil, code, 100000, 1, &steps)
	if kind != execOK {
		t.Fatalf("insufficient value kind=%d", kind)
	}

	st.SetAccount(Account{Address: contract, Balance: U256(10)})
	st.SetAccount(Account{Address: to, Balance: maxUint256Minus(1)})
	_, _, _, kind = runCode(st, contract, Address{}, U256(0), nil, code, 100000, 1, &steps)
	if kind != execHalt {
		t.Fatalf("overflow kind=%d", kind)
	}
}

func TestCallDepthGasAndWideArgs(t *testing.T) {
	m := &machine{state: NewState(), depth: maxDepth, stack: make([]Hash, 7), gas: 100000}
	if err := m.opCall(); err != nil {
		t.Fatal(err)
	}
	if m.stack[len(m.stack)-1] != (Hash{}) {
		t.Fatalf("depth result = %x", m.stack[len(m.stack)-1])
	}

	var to Address
	to[19] = 2
	wide := Hash{0: 1}
	code := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x73}
	code = append(code, to[:]...)
	code = append(code, 0x7f)
	code = append(code, wide[:]...)
	code = append(code, 0xf1)
	_, _, _, kind := runVM(t, code, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("wide gas kind=%d", kind)
	}

	lowGas := callStack(to, 50, 0, 0, 0, 0, 0)
	_, _, _, kind = runVM(t, lowGas, 40, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("call gas kind=%d", kind)
	}

	growIn := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x01, 0x62, 0x04, 0x00, 0x00, 0x60, 0x00, 0x73}
	growIn = append(growIn, to[:]...)
	growIn = append(growIn, 0x60, 50, 0xf1)
	_, _, _, kind = runVM(t, growIn, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("call in grow kind=%d", kind)
	}

	growOut := []byte{0x60, 0x01, 0x62, 0x04, 0x00, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x73}
	growOut = append(growOut, to[:]...)
	growOut = append(growOut, 0x60, 50, 0xf1)
	_, _, _, kind = runVM(t, growOut, 100000, maxSteps, nil)
	if kind != execHalt {
		t.Fatalf("call out grow kind=%d", kind)
	}
}

func TestPopUint64EmptyStack(t *testing.T) {
	m := &machine{}
	if _, ok := m.popUint64(); ok {
		t.Fatal("empty stack")
	}
	if _, ok := hashUint64(Hash{0: 1}); ok {
		t.Fatal("wide word")
	}
}

func TestMemoryExpandsByWords(t *testing.T) {
	m := &machine{gas: 100}
	if err := m.grow(0, 1); err != nil {
		t.Fatal(err)
	}
	if len(m.mem) != 32 || m.gas != 97 {
		t.Fatalf("len=%d gas=%d", len(m.mem), m.gas)
	}
	if err := m.grow(0, 32); err != nil {
		t.Fatal(err)
	}
	if len(m.mem) != 32 || m.gas != 97 {
		t.Fatalf("inside word len=%d gas=%d", len(m.mem), m.gas)
	}
	if err := m.grow(32, 1); err != nil {
		t.Fatal(err)
	}
	// 两个字的总费用是 6，比上次多 3。
	if len(m.mem) != 64 || m.gas != 94 {
		t.Fatalf("second word len=%d gas=%d", len(m.mem), m.gas)
	}
	if err := m.grow(maxMemorySize, 1); err == nil {
		t.Fatal("memory past the evm limit was accepted")
	}
}

func TestCallStipendRunsOnZeroGas(t *testing.T) {
	st := NewState()
	var caller, callee Address
	caller[19] = 1
	callee[19] = 2
	st.SetAccount(Account{Address: caller, Balance: U256(5)})
	// 子调用只转发 0 gas。没有 stipend 时第一条 PUSH 就停机，CALL 结果是 0。
	st.SetCode(callee, []byte{0x60, 0x01, 0x00})
	code := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x01, 0x73}
	code = append(code, callee[:]...)
	code = append(code, 0x60, 0x00, 0xf1, 0x60, 0x00, 0x55, 0x00)
	steps := maxSteps
	_, _, _, kind := runCode(st, caller, Address{}, U256(0), nil, code, 100000, 1, &steps)
	if kind != execOK {
		t.Fatalf("kind=%d", kind)
	}
	if st.GetStorage(caller, Hash{}) != hashFromUint64(1) {
		t.Fatalf("call result = %x", st.GetStorage(caller, Hash{}))
	}
}

func TestMachineChargeStopsBeforeFn(t *testing.T) {
	called := false
	m := &machine{gas: 1}
	err := m.charge(gasBase, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, errHalt) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
