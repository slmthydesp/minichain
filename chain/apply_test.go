package chain

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

// funded 建一个只有演示账户的状态。余额和 nonce 由调用方给定。
func funded(t *testing.T, balance, nonce uint64) (*State, Address) {
	t.Helper()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	var addr Address
	copy(addr[:], raw[:])
	st := NewState()
	st.SetAccount(Account{Address: addr, Balance: U256(balance), Nonce: nonce})
	return st, addr
}

// 转 40，单价 1。发送方扣掉 40 加 21000，nonce 变成 1。收款方得到 40，nonce 仍是 0。
func TestApplyTransferUpdatesNonceAndBalances(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var to Address
	to[19] = 9
	tx, err := Sign(demoKey(t), to, 40, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	// 发送方扣掉 40 和 21000，nonce 变成 1。收款方只增加 40。
	got, ok := st.GetAccount(from)
	if !ok || got.Balance != U256(100000-40-21000) || got.Nonce != 1 {
		t.Fatalf("sender = %+v ok=%v", got, ok)
	}
	recip, ok := st.GetAccount(to)
	if !ok || recip.Balance != U256(40) || recip.Nonce != 0 {
		t.Fatalf("recipient = %+v ok=%v", recip, ok)
	}
}

// to 是 20 个 0。这是转给零地址，不是创建合约。
// 零地址收到 10，没有代码。发送方扣掉金额和 21000，nonce 加 1。
func TestApplyZeroAddressIsTransferNotCreate(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var coin Address
	coin[19] = 3
	st.SetCoinbase(coin)
	// to 写成全 0。Create 仍是假，所以这是转账。
	tx, err := Sign(demoKey(t), Address{}, 10, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	// 零地址收到 10，nonce 仍是 0，上面没有代码。
	recip, ok := st.GetAccount(Address{})
	if !ok || recip.Balance != U256(10) || recip.Nonce != 0 || len(st.GetCode(Address{})) != 0 {
		t.Fatalf("zero address = %+v ok=%v", recip, ok)
	}
	got, _ := st.GetAccount(from)
	if got.Balance != U256(100000-10-21000) || got.Nonce != 1 {
		t.Fatalf("sender = %+v", got)
	}
}

// 转给自己。金额扣掉又加回来，余额只少 21000。这 21000 进 coinbase，发送方 nonce 加 1。
func TestApplySelfTransferKeepsBalance(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var coin Address
	coin[19] = 5
	st.SetCoinbase(coin)
	tx, err := Sign(demoKey(t), from, 5, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	// 金额扣掉又加回自己。余额只少给 coinbase 的 21000。
	got, _ := st.GetAccount(from)
	if got.Balance != U256(100000-21000) || got.Nonce != 1 {
		t.Fatalf("self = %+v", got)
	}
	paid, ok := st.GetAccount(coin)
	if !ok || paid.Balance != U256(21000) {
		t.Fatalf("coinbase = %+v ok=%v", paid, ok)
	}
}

// 账户 nonce 是 1，交易 nonce 是 0，号码对不上。
// 余额只有 10，转 11 再加费用也不够。两种都返回错误，账户保持执行前的样子。
func TestApplyRejectsWithoutMutating(t *testing.T) {
	var to Address
	to[19] = 3
	tests := []struct {
		name    string
		balance uint64
		tx      func() Tx
		want    error
	}{
		// 账户 nonce 是 1，交易 nonce 是 0。
		{"nonce", 100000, func() Tx {
			tx, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
			if err != nil {
				t.Fatal(err)
			}
			return tx
		}, ErrNonceMismatch},
		// 余额 10，转 11 再加费用，不够。
		{"balance", 10, func() Tx {
			tx, err := Sign(demoKey(t), to, 11, 1, 21000, 1, ChainID)
			if err != nil {
				t.Fatal(err)
			}
			return tx
		}, ErrInsufficientBalance},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, from := funded(t, tt.balance, 1)
			before, _ := st.GetAccount(from)
			_, err := Apply(st, tt.tx())
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v", err)
			}
			// 错误返回之后，账户要和执行前一样。
			got, _ := st.GetAccount(from)
			if got != before {
				t.Fatalf("state changed to %+v", got)
			}
		})
	}
}

// 收款方余额离上限只差 1，再转入 2 会溢出。发送方和收款方都保持原样。
func TestApplyRejectsRecipientOverflow(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var to Address
	to[19] = 4
	// 收款方再收 2 就会超过 2^256-1。
	st.SetAccount(Account{Address: to, Balance: maxUint256Minus(1), Nonce: 0})
	senderBefore, _ := st.GetAccount(from)
	tx, err := Sign(demoKey(t), to, 2, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("err = %v", err)
	}
	got, _ := st.GetAccount(to)
	if got.Balance != maxUint256Minus(1) || got.Nonce != 0 {
		t.Fatalf("recipient = %+v", got)
	}
	senderAfter, _ := st.GetAccount(from)
	if senderAfter != senderBefore {
		t.Fatalf("sender changed to %+v", senderAfter)
	}
}

// 签好之后把签名首字节改成 0。Apply 返回错误。
// 发送方、另一个已有账户都不变，收款方也不会被建出来。
func TestApplyRejectsBadSignatureWithoutMutating(t *testing.T) {
	st, from := funded(t, 10, 0)
	var other Address
	other[19] = 8
	st.SetAccount(Account{Address: other, Balance: U256(4), Nonce: 1})
	var to Address
	to[19] = 3
	tx, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 签好之后改坏签名。发送方恢复不出来。
	tx.Sig = bytes.Clone(tx.Sig)
	tx.Sig[0] = 0
	senderBefore, _ := st.GetAccount(from)
	otherBefore, _ := st.GetAccount(other)
	if _, err := Apply(st, tx); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v", err)
	}
	senderAfter, _ := st.GetAccount(from)
	otherAfter, _ := st.GetAccount(other)
	if senderAfter != senderBefore || otherAfter != otherBefore {
		t.Fatal("bad signature changed state")
	}
	// 失败的交易不能把收款方建出来。
	if _, ok := st.GetAccount(to); ok {
		t.Fatal("bad signature created the recipient")
	}
}

// 单价 2 的转账实际烧掉 21000，费用 42000 进 coinbase。发送方再扣掉转出的 10。
// gasLimit 写成 40000 时，普通转账仍只烧 21000，费用还是 42000。
// gasLimit 写成 20999，不够起步 gas。状态不动，收款方和 coinbase 都不会新建，池也拒绝这笔。
func TestApplyPaysCoinbase(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	var to, coin Address
	to[19] = 9
	coin[19] = 4
	st.SetCoinbase(coin)
	tx, err := Sign(demoKey(t), to, 10, 0, 21000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := Apply(st, tx)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.GasUsed != IntrinsicGas || receipt.Status != 1 {
		t.Fatalf("receipt = %+v", receipt)
	}
	got, ok := st.GetAccount(from)
	if !ok || got.Balance != U256(1000000-10-42000) || got.Nonce != 1 {
		t.Fatalf("sender = %+v ok=%v", got, ok)
	}
	recip, ok := st.GetAccount(to)
	if !ok || recip.Balance != U256(10) || recip.Nonce != 0 {
		t.Fatalf("recipient = %+v ok=%v", recip, ok)
	}
	// 21000 乘单价 2，费用 42000 进 coinbase。
	miner, ok := st.GetAccount(coin)
	if !ok || miner.Balance != U256(42000) || miner.Nonce != 0 {
		t.Fatalf("coinbase = %+v ok=%v", miner, ok)
	}

	// gasLimit 比实际消耗大。收费仍按烧掉的 21000 乘单价。
	wideState, wideFrom := funded(t, 1000000, 0)
	wideState.SetCoinbase(coin)
	wide, err := Sign(demoKey(t), to, 10, 0, 40000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = Apply(wideState, wide)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.GasUsed != 21000 {
		t.Fatalf("wide gas = %d", receipt.GasUsed)
	}
	wideSender, _ := wideState.GetAccount(wideFrom)
	if wideSender.Balance != U256(1000000-10-42000) {
		t.Fatalf("wide sender balance = %d", wideSender.Balance)
	}
	wideCoin, _ := wideState.GetAccount(coin)
	if wideCoin.Balance != U256(42000) {
		t.Fatalf("wide coinbase balance = %d", wideCoin.Balance)
	}

	// 起步 gas 不够。执行前的状态原样留下。
	lowState, lowFrom := funded(t, 1000000, 0)
	before, _ := lowState.GetAccount(lowFrom)
	root := StateRoot(lowState)
	low, err := Sign(demoKey(t), to, 10, 0, 20999, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(lowState, low); !errors.Is(err, ErrIntrinsicGas) || err.Error() != "intrinsic gas" {
		t.Fatalf("err = %v", err)
	}
	after, _ := lowState.GetAccount(lowFrom)
	if after != before || StateRoot(lowState) != root {
		t.Fatalf("state changed to %+v", after)
	}
	if _, ok := lowState.GetAccount(to); ok {
		t.Fatal("intrinsic gas created the recipient")
	}
	if _, ok := lowState.GetAccount(coin); ok {
		t.Fatal("intrinsic gas created the coinbase")
	}
	// 池和 Apply 用同一条起步 gas 规则，这笔也进不了池。
	if err := NewPool().AddTx(low, U256(1000000), 0); !errors.Is(err, ErrIntrinsicGas) {
		t.Fatalf("pool err = %v", err)
	}
}

// 发送方就是 coinbase。费用先扣再加回，余额只少转出的 10，nonce 加 1。
// 再转给自己时，金额和费用都回到同一账户，余额不变，nonce 仍加 1。
func TestApplyRefundsFeeWhenSenderIsCoinbase(t *testing.T) {
	st, from := funded(t, 100000, 0)
	// 费用的收款方就是发送方本人。扣掉的费用会加回来。
	st.SetCoinbase(from)
	var to Address
	to[19] = 9
	tx, err := Sign(demoKey(t), to, 10, 0, 21000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetAccount(from)
	if got.Balance != U256(100000-10) || got.Nonce != 1 {
		t.Fatalf("sender coinbase = %+v", got)
	}
	recip, ok := st.GetAccount(to)
	if !ok || recip.Balance != U256(10) {
		t.Fatalf("recipient = %+v ok=%v", recip, ok)
	}

	// 收款方也是自己。转出的 5 同样加回来。
	self, selfFrom := funded(t, 100000, 0)
	self.SetCoinbase(selfFrom)
	tx, err = Sign(demoKey(t), selfFrom, 5, 0, 21000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(self, tx); err != nil {
		t.Fatal(err)
	}
	got, _ = self.GetAccount(selfFrom)
	if got.Balance != U256(100000) || got.Nonce != 1 {
		t.Fatalf("self coinbase = %+v", got)
	}
}

// 创建地址上已经有 nonce 1，这次创建会停机。停机本应按整个 gasLimit 向 coinbase 收费。
// 但 coinbase 余额已是最大值，入账溢出。收费失败不是回执，整笔换回执行前的快照。
// 发送方 nonce 不会留下 +1，coinbase 和那个占用地址也都保持原样。
func TestApplyHaltFeeOverflowRestoresSnapshot(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var coin Address
	coin[19] = 4
	st.SetCoinbase(coin)
	// coinbase 已经满了。后面停机费用是 gasLimit * 1，加进去会溢出。
	st.SetAccount(Account{Address: coin, Balance: MaxUint256()})
	// 创建地址用发送方当前 nonce 0 算：CreateAddress(from, 0)。
	occupied := CreateAddress(from, 0)
	// nonce 1 是这里写进去的，不是发送方的 nonce。executeCreate 看到目标账户 nonce > 0 就当已有合约，整笔停机。
	st.SetAccount(Account{Address: occupied, Nonce: 1})
	senderBefore, _ := st.GetAccount(from)
	root := StateRoot(st)
	// nonce 0、value 0、gasLimit 53000、单价 1。没有初始代码，创建地址已被占用就会停机。
	tx, err := SignCreate(demoKey(t), 0, 0, TxGasContractCreation, 1, ChainID, nil)
	if err != nil {
		t.Fatal(err)
	}
	// executeTx 先把 from 的 nonce 加 1，发现地址占用后停机；commitExec 换回快照再加一次 nonce。
	// receiptFor 按整个 gasLimit 收费时 coinbase 溢出，Apply 再换回执行前快照，nonce 的 +1 也丢掉。
	if _, err := Apply(st, tx); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("err = %v", err)
	}
	senderAfter, _ := st.GetAccount(from)
	coinAfter, _ := st.GetAccount(coin)
	occupiedAfter, _ := st.GetAccount(occupied)
	// from 余额和 nonce 都回到执行前。coinbase 仍是最大值。占用地址仍是 nonce 1。
	if senderAfter != senderBefore || coinAfter.Balance != MaxUint256() || occupiedAfter.Nonce != 1 || StateRoot(st) != root {
		t.Fatalf("halt fee overflow changed state: sender=%+v coin=%d occupied=%+v", senderAfter, coinAfter.Balance, occupiedAfter)
	}
}

// 转账本身可以执行：from 会扣款、nonce 加 1，收款方会被建出来。
// 但 coinbase 余额已是最大值，费用入账溢出。收费失败换回快照，这笔不入块。
// 发送方不变，收款方也不会留在状态里。
func TestApplyRejectsCoinbaseOverflow(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var to, coin Address
	to[19] = 9
	coin[19] = 4
	st.SetCoinbase(coin)
	// 转账可以做完。费用是 21000 * 1，coinbase 再收就会超出 2^256-1。
	st.SetAccount(Account{Address: coin, Balance: MaxUint256(), Nonce: 0})
	senderBefore, _ := st.GetAccount(from)
	root := StateRoot(st)
	tx, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 执行阶段已经改过 from 和 to。payFee 溢出后 Apply 换回快照，这些改动都不留下。
	if _, err := Apply(st, tx); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("err = %v", err)
	}
	senderAfter, _ := st.GetAccount(from)
	coinAfter, _ := st.GetAccount(coin)
	// from 的余额和 nonce 都回到执行前。coinbase 仍是最大值，状态根也不变。
	if senderAfter != senderBefore || coinAfter.Balance != MaxUint256() || StateRoot(st) != root {
		t.Fatal("coinbase overflow changed state")
	}
	// 快照换回之后，收款方账户不存在。
	if _, ok := st.GetAccount(to); ok {
		t.Fatal("coinbase overflow created the recipient")
	}
}

// 转 10，单价 0。费用是 0，不扣 gas。发送方只少 10，nonce 变成 1。
func TestApplyZeroGasPriceSkipsFee(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var to Address
	to[19] = 9
	// gasPrice 写成 0。21000 gas 乘出来的费用仍是 0。
	tx, err := Sign(demoKey(t), to, 10, 0, 21000, 0, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	// 余额只少转出的 10，没有 21000 的手续费。nonce 加 1。
	got, _ := st.GetAccount(from)
	if got.Balance != U256(100000-10) || got.Nonce != 1 {
		t.Fatalf("sender = %+v", got)
	}
}

// 创建失败的四条路。初始代码 REVERT 时不部署，回执状态为 0。
// 创建地址余额再加上 value 溢出时是错误，整笔不入块。
// RETURN 要写出的内存扩不开，或剩下的 gas 不够按字节交部署费，都停机，状态为 0。
func TestCreateHaltPaths(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	key := demoKey(t)
	// PUSH1 0、PUSH1 0、REVERT。初始代码自己回退，创建地址上不留代码。
	revert, err := SignCreate(key, 0, 0, 90000, 1, ChainID, []byte{0x60, 0x00, 0x60, 0x00, 0xfd})
	if err != nil {
		t.Fatal(err)
	}
	receipt := mustApply(t, st, revert)
	if receipt.Status != 0 || len(st.GetCode(CreateAddress(from, 0))) != 0 {
		t.Fatalf("revert create = %+v", receipt)
	}

	st, from = funded(t, 1000000, 0)
	addr := CreateAddress(from, 0)
	// 创建地址上已经有 2^256-2。再转入 2 会超出 2^256-1，Apply 换回快照。
	st.SetAccount(Account{Address: addr, Balance: maxUint256Minus(1)})
	create, err := SignCreate(key, 2, 0, 90000, 1, ChainID, []byte{0x00})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, create); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("overflow err = %v", err)
	}

	st, _ = funded(t, 1000000, 0)
	// PUSH2 0x6001、PUSH1 0、RETURN。要返回 24577 字节，超过代码长度上限，停机。
	huge := []byte{0x61, 0x60, 0x01, 0x60, 0x00, 0xf3}
	create, err = SignCreate(key, 0, 0, 90000, 1, ChainID, huge)
	if err != nil {
		t.Fatal(err)
	}
	receipt = mustApply(t, st, create)
	if receipt.Status != 0 {
		t.Fatalf("code size status = %d", receipt.Status)
	}

	st, _ = funded(t, 1000000, 0)
	// PUSH1 1、PUSH1 0、RETURN。只返回 1 字节，但 gasLimit 只比起步 gas 多 100，不够再付 200 的部署费。
	tiny := []byte{0x60, 0x01, 0x60, 0x00, 0xf3}
	create, err = SignCreate(key, 0, 0, TxGasContractCreation+100, 1, ChainID, tiny)
	if err != nil {
		t.Fatal(err)
	}
	receipt = mustApply(t, st, create)
	if receipt.Status != 0 {
		t.Fatalf("deposit status = %d", receipt.Status)
	}
}

// receiptFor 按用掉的 gas 乘单价。gasLimit 是最大值、gasLeft 是 0 时，乘以 2 放不进 uint64。
// payFee 扣 1 时发送方还不存在，余额是 0。费用为 0 时不动账户。
func TestReceiptForAndPayFeeErrors(t *testing.T) {
	st := NewState()
	var sender Address
	sender[19] = 1
	// 用掉的 gas 是 MaxUint64。单价 2，gasFee 溢出，还没扣到余额。
	_, err := receiptFor(st, sender, Tx{GasLimit: math.MaxUint64, GasPrice: 2}, 1, 0, nil)
	if !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("fee overflow err = %v", err)
	}
	// 这个地址不在状态里，余额按 0 算，扣 1 失败。
	if err := payFee(st, sender, 1); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("payFee err = %v", err)
	}
	// 费用是 0，直接返回，不建账户。
	if err := payFee(st, sender, 0); err != nil {
		t.Fatal(err)
	}
}
