package chain

import (
	"bytes"
	"testing"
)

// 这个文件测分叉选择、换链头时交易池怎么改，以及最终确定之后从更早处分出去的叉能不能当链头。
// 链头先比高度，更高的赢；一样高则哈希字节更小的赢。输掉的块仍保存在 blocks 里。

// 两条链从同一个创世块各挖一块，coinbase 不同，高度同为 1，哈希不同。
// 观察者先收下这两块：同高度时哈希字节更小的当链头，另一块也留着。
// 右边再挖一块变成高度 2。观察者接上之后，更长的右边当链头，规范链是右边那两块。
func TestForkChoicePrefersSmallerHashThenLongerChain(t *testing.T) {
	var leftCoin, rightCoin Address
	leftCoin[19] = 1
	rightCoin[19] = 2
	leftChain := New()
	rightChain := New()
	// coinbase 写进区块头，左右两块哈希因此不同。
	leftBlock, err := leftChain.Mine(leftCoin, leftChain.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	rightBlock, err := rightChain.Mine(rightCoin, rightChain.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	// 观察者从创世块开始，依次接上左右两块。先后顺序不影响同高度比哈希。
	obs := New()
	if err := obs.Append(leftBlock); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(rightBlock); err != nil {
		t.Fatal(err)
	}
	leftHash := leftBlock.Hash()
	rightHash := rightBlock.Hash()
	want := leftHash
	if bytes.Compare(rightHash[:], leftHash[:]) < 0 {
		want = rightHash
	}
	if obs.Head().Hash() != want {
		t.Fatalf("head = %s want %s", obs.Head().Hash().Hex(), want.Hex())
	}
	// 输掉的那块也留着，后面它的子块还能接上来。
	if !obs.HasBlock(leftBlock.Hash()) || !obs.HasBlock(rightBlock.Hash()) {
		t.Fatal("a competing block was discarded")
	}

	// 右边再长高一块。高度比哈希优先，链头改到这块。
	extra, err := rightChain.Mine(rightCoin, rightChain.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(extra); err != nil {
		t.Fatal(err)
	}
	if obs.Head().Hash() != extra.Hash() {
		t.Fatalf("head = %s want longer %s", obs.Head().Hash().Hex(), extra.Hash().Hex())
	}
	// BlocksAfter(0) 只走当前链头这条规范链，不含创世块。左边那块不在结果里。
	got := obs.BlocksAfter(0)
	if len(got) != 2 || got[0].Hash() != rightBlock.Hash() || got[1].Hash() != extra.Hash() {
		t.Fatalf("canonical blocks = %d", len(got))
	}
}

// 短链的一块里有一笔转账。长链是两块空块，高度更高。
// 观察者先接上短链：转账出池，收款方入账。
// 再接上长链后换链头。这笔转账回到池里，收款方余额回到 0，短链那块仍留着。
func TestReorgRestoresAbandonedTransfer(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 9
	tx, err := Sign(key, to, 10, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	var shortCoin, longCoin Address
	shortCoin[19] = 1
	longCoin[19] = 2

	short := New()
	if _, err := short.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	spent, err := short.Mine(shortCoin, short.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(spent.Txs) != 1 {
		t.Fatalf("short block txs = %d", len(spent.Txs))
	}

	// 长链不装这笔转账，连挖两块空块。高度 2 一定比短链的高度 1 大。
	long := New()
	first, err := long.Mine(longCoin, long.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := long.Mine(longCoin, long.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}

	// 观察者池里先有这笔转账，接上短链后它入块出池。Account 读的是当前链头状态。
	obs := New()
	if _, err := obs.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(spent); err != nil {
		t.Fatal(err)
	}
	if len(obs.Pool().PendingTxs()) != 0 {
		t.Fatal("included transaction still pending")
	}
	if acct, _ := obs.Account(to); acct.Balance != 10 {
		t.Fatalf("balance = %d", acct.Balance)
	}
	// 长链更高。第一块同高，先比哈希；第二块高度更大，链头换成它。
	if err := obs.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(second); err != nil {
		t.Fatal(err)
	}
	if obs.Head().Hash() != second.Hash() {
		t.Fatal("longer chain did not become head")
	}
	// 换链头时，旧叉上这笔还没在新叉花掉同一 nonce，按新状态放回池。
	if len(obs.Pool().PendingTxs()) != 1 {
		t.Fatalf("pending after reorg = %d", len(obs.Pool().PendingTxs()))
	}
	// 链头状态换成空块那条，收款方在规范链上没有入账。
	if acct, _ := obs.Account(to); acct.Balance != 0 {
		t.Fatalf("balance after reorg = %d", acct.Balance)
	}
	// 被放弃的短链块仍保存在 blocks 里，只是不再当链头。
	if !obs.HasBlock(spent.Hash()) {
		t.Fatal("abandoned block was dropped")
	}
}

// 同一发送方的两笔交易都用 nonce 0，收款方不同。
// 短链装第一笔。长链的第一块装第二笔，再挖一块空块，高度更高。
// 换到长链后，nonce 0 已经在赢的那条链上花掉，短链那笔不再放回池。
func TestReorgDoesNotRestoreNonceSpentOnWinner(t *testing.T) {
	key := demoKey(t)
	var toA, toB Address
	toA[19] = 1
	toB[19] = 2
	// 同一发送方、同一 nonce 0，金额和收款方不同，所以是两笔不同的交易。
	txA, err := Sign(key, toA, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	txB, err := Sign(key, toB, 2, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	var shortCoin, longCoin Address
	shortCoin[19] = 3
	longCoin[19] = 4

	short := New()
	if _, err := short.AddTx(txA); err != nil {
		t.Fatal(err)
	}
	spent, err := short.Mine(shortCoin, short.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	long := New()
	if _, err := long.AddTx(txB); err != nil {
		t.Fatal(err)
	}
	first, err := long.Mine(longCoin, long.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := long.Mine(longCoin, long.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}

	obs := New()
	if _, err := obs.AddTx(txA); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(spent); err != nil {
		t.Fatal(err)
	}
	// 先接短链，txA 入块。再接长链两块，链头换成装了 txB 的那条。
	if err := obs.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(second); err != nil {
		t.Fatal(err)
	}
	// 赢的链已经用掉 nonce 0。adopt 不会把旧叉上同一号码的 txA 放回池。
	if len(obs.Pool().PendingTxs()) != 0 {
		t.Fatal("nonce spent on the winning chain returned to the pool")
	}
	// 规范链上入账的是 txB 的收款方。toA 在当前链头看不到这笔。
	if acct, _ := obs.Account(toB); acct.Balance != 2 {
		t.Fatalf("winner recipient = %d", acct.Balance)
	}
	if acct, _ := obs.Account(toA); acct.Balance == 1 {
		t.Fatalf("loser recipient = %d", acct.Balance)
	}
}

// 同一块接两次。第二次直接成功，链头的高度和哈希都不变。
func TestAppendDuplicateIsNoop(t *testing.T) {
	var coin Address
	coin[19] = 5
	fresh := New()
	block, err := fresh.Mine(coin, fresh.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	obs := New()
	if err := obs.Append(block); err != nil {
		t.Fatal(err)
	}
	// 同一哈希已经在 blocks 里，第二次直接返回，不再重放，也不再比链头。
	if err := obs.Append(block); err != nil {
		t.Fatal(err)
	}
	if obs.Head().Height != 1 || obs.Head().Hash() != block.Hash() {
		t.Fatal("duplicate append changed the head")
	}
}

// 挖出一块后改掉父哈希。观察者没有这个父块，拒绝整块。
// 链头仍是创世块，被拒绝的块也不保存。
func TestAppendRejectsUnknownParent(t *testing.T) {
	var coin Address
	coin[19] = 6
	fresh := New()
	block, err := fresh.Mine(coin, fresh.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	// 父哈希改一个字节，哈希也变了。观察者按这个字段去找父块，找不到就拒绝。
	block.ParentHash[0] ^= 0xff
	obs := New()
	if err := obs.Append(block); err == nil {
		t.Fatal("expected unknown parent")
	}
	// 写入发生在校验之后。拒绝时 blocks 里没有这块，链头仍是创世块。
	if obs.Head().Height != 0 || obs.HasBlock(block.Hash()) {
		t.Fatal("rejected block was stored")
	}
}

// 规范链挖到高度 13，最终确定的是高度 1 那块。
// 侧链从同一个创世块另起，coinbase 不同，所以在高度 1 就分岔，分岔点在最终确定区块前面。
// 侧链一直挖到比规范链更高，并逐块接上。它会被保存，但不能成为链头。
func TestFinalityKeepsForkBelowCheckpointOffTheHead(t *testing.T) {
	canon := New()
	var coin Address
	coin[19] = 1
	// FinalityDepth 是 12。挖到高度 13 时，最终确定的是高度 13-12=1 那块。
	for canon.Head().Height < FinalityDepth+1 {
		if _, err := canon.Mine(coin, canon.Head().Time+1); err != nil {
			t.Fatal(err)
		}
	}
	head := canon.Head().Hash()
	// 侧链从创世块另起，第一块的父块是高度 0。eligible 只留最终确定区块和它的后代，创世块不在里面。
	side := New()
	var other Address
	other[19] = 2
	// 一直挖到比规范链还高。prefer 看到父块不在 eligible 里就直接返回，这些块当不成链头。
	for side.Head().Height <= canon.Head().Height+1 {
		block, err := side.Mine(other, side.Head().Time+1)
		if err != nil {
			t.Fatal(err)
		}
		// side 挖的块直接写入 canon 的 blocks
		if err := canon.Append(block); err != nil {
			t.Fatal(err)
		}
	}
	if canon.Head().Hash() != head {
		t.Fatal("fork below the checkpoint became head")
	}
	// 块仍然写入 blocks。HasBlock 为真，只是 Head 不跟着走。
	if !canon.HasBlock(side.Head().Hash()) {
		t.Fatal("side block was not stored")
	}
}

// 规范链同样挖到高度 13，最终确定的是它的高度 1。
// 竞争链先接上这块高度 1，再从这里继续挖，直到比规范链高。
// 分岔点就在最终确定区块上。这些新块按从旧到新接上后，更长的竞争链成为链头。
func TestLongerForkAboveCheckpointCanBecomeHead(t *testing.T) {
	canon := New()
	var coin Address
	coin[19] = 1
	var shared Block
	for canon.Head().Height < FinalityDepth+1 {
		block, err := canon.Mine(coin, canon.Head().Time+1)
		if err != nil {
			t.Fatal(err)
		}
		if block.Height == 1 {
			shared = block
		}
	}
	// 竞争链先接上规范链已经最终确定的那块高度 1。后面的父块都在 eligible 里，可以比链头。
	rival := New()
	if err := rival.Append(shared); err != nil {
		t.Fatal(err)
	}
	var other Address
	other[19] = 9
	var extra []Block
	// 从高度 2 起换 coinbase，哈希和规范链分开。一直挖到比规范链更高。
	for rival.Head().Height <= canon.Head().Height {
		block, err := rival.Mine(other, rival.Head().Time+1)
		if err != nil {
			t.Fatal(err)
		}
		extra = append(extra, block)
	}
	// 必须从旧到新接。未知父块会被拒绝。最后一块高度更大，成为链头。
	for _, block := range extra {
		if err := canon.Append(block); err != nil {
			t.Fatal(err)
		}
	}
	if canon.Head().Hash() != rival.Head().Hash() {
		t.Fatal("longer fork above the checkpoint did not become head")
	}
}
