package chain

import "testing"

func TestWordEdges(t *testing.T) {
	one := hashFromUint64(1)
	two := hashFromUint64(2)
	neg := hashOnes()
	wide := Hash{0: 1}

	if sdivWord(one, Hash{}) != (Hash{}) {
		t.Fatal("sdiv by zero")
	}
	if sdivWord(neg, one) != neg {
		t.Fatal("sdiv -1/1")
	}
	if smodWord(one, Hash{}) != (Hash{}) {
		t.Fatal("smod by zero")
	}
	if smodWord(neg, two) != neg {
		t.Fatal("smod sign follows dividend")
	}
	if mulModWord(one, one, Hash{}) != (Hash{}) {
		t.Fatal("mulmod zero")
	}
	if signExtendWord(wide, one) != one || signExtendWord(hashFromUint64(32), one) != one {
		t.Fatal("signextend passthrough")
	}
	num := Hash{31: 0x80}
	got := signExtendWord(Hash{}, num)
	if got[0] != 0xff || got[31] != 0x80 {
		t.Fatalf("signextend %x", got)
	}
	if sltWord(one, one) != (Hash{}) || sltWord(neg, one) != one {
		t.Fatal("slt")
	}
	if sgtWord(one, one) != (Hash{}) || sgtWord(one, neg) != one {
		t.Fatal("sgt")
	}
	if shrWord(wide, one) != (Hash{}) || shrWord(hashFromUint64(256), one) != (Hash{}) {
		t.Fatal("shr overflow")
	}
	if sarWord(wide, one) != (Hash{}) || sarWord(hashFromUint64(256), one) != (Hash{}) {
		t.Fatal("sar positive overflow")
	}
	if sarWord(hashFromUint64(256), neg) != neg {
		t.Fatal("sar negative overflow")
	}
}
