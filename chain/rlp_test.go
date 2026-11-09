package chain

import (
	"bytes"
	"testing"
)

func TestRLPString(t *testing.T) {
	// length: 56
	const lorem = "Lorem ipsum dolor sit amet, consectetur adipisicing elit"
	if len(lorem) != 56 {
		t.Fatalf("lorem len = %d", len(lorem))
	}
	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{name: "empty", in: nil, want: []byte{0x80}},
		{name: "single below 0x80", in: []byte{0x00}, want: []byte{0x00}},
		{name: "single 0x7f", in: []byte{0x7f}, want: []byte{0x7f}},
		{name: "single 0x80", in: []byte{0x80}, want: []byte{0x81, 0x80}},
		{name: "dog", in: []byte("dog"), want: []byte{0x83, 'd', 'o', 'g'}},
		{
			name: "55 bytes still short",
			in:   bytes.Repeat([]byte{'a'}, 55),
			want: append([]byte{0xb7}, bytes.Repeat([]byte{'a'}, 55)...),
		},
		{
			name: "56 byte lorem is long",
			in:   []byte(lorem),
			// 0xb8-0xb7=1, 长度 1 字节，0x38=56
			want: append([]byte{0xb8, 0x38}, lorem...),
		},
		{
			name: "256 bytes length takes two bytes",
			in:   bytes.Repeat([]byte{'b'}, 256),
			// 0xb9-0xb7=2, 长度 2 字节，0x0100=256
			want: append([]byte{0xb9, 0x01, 0x00}, bytes.Repeat([]byte{'b'}, 256)...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rlpString(tt.in); !bytes.Equal(got, tt.want) {
				t.Fatalf("got %x want %x", got, tt.want)
			}
		})
	}
}

func TestRLPUint(t *testing.T) {
	tests := []struct {
		name string
		n    uint64
		want []byte
	}{
		{name: "zero", n: 0, want: []byte{0x80}},
		{name: "fifteen", n: 15, want: []byte{0x0f}},
		{name: "127", n: 127, want: []byte{0x7f}},
		{name: "128", n: 128, want: []byte{0x81, 0x80}},
		{name: "1024", n: 1024, want: []byte{0x82, 0x04, 0x00}},
		{name: "max", n: ^uint64(0), want: []byte{0x88, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rlpUint(tt.n); !bytes.Equal(got, tt.want) {
				t.Fatalf("got %x want %x", got, tt.want)
			}
		})
	}
}

func TestRLPList(t *testing.T) {
	dog := []byte{0x83, 'd', 'o', 'g'}
	lorem := append([]byte{0xb8, 0x38}, "Lorem ipsum dolor sit amet, consectetur adipisicing elit"...)
	tests := []struct {
		name  string
		items [][]byte
		want  []byte
	}{
		{name: "empty", items: nil, want: []byte{0xc0}},
		{
			name:  "cat and dog",
			items: [][]byte{{0x83, 'c', 'a', 't'}, dog},
			want:  []byte{0xc8, 0x83, 'c', 'a', 't', 0x83, 'd', 'o', 'g'},
		},
		{
			name:  "payload 55 still short",
			items: repeatItem([]byte{0x01}, 55),
			want:  append([]byte{0xf7}, bytes.Repeat([]byte{0x01}, 55)...),
		},
		{
			name:  "payload 56 is long",
			items: repeatItem([]byte{0x01}, 56),
			want:  append([]byte{0xf8, 56}, bytes.Repeat([]byte{0x01}, 56)...),
		},
		{
			name:  "13 dogs still short",
			items: repeatItem(dog, 13),
			want:  append([]byte{0xf4}, bytes.Repeat(dog, 13)...),
		},
		{
			name:  "14 dogs is long",
			items: repeatItem(dog, 14),
			want:  append([]byte{0xf8, 56}, bytes.Repeat(dog, 14)...),
		},
		{
			name:  "one long string",
			items: [][]byte{lorem},
			want:  append([]byte{0xf8, byte(len(lorem))}, lorem...),
		},
		{
			name:  "payload 256 length takes two bytes",
			items: repeatItem([]byte{0x01}, 256),
			want:  append([]byte{0xf9, 0x01, 0x00}, bytes.Repeat([]byte{0x01}, 256)...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rlpList(tt.items...); !bytes.Equal(got, tt.want) {
				t.Fatalf("got %x want %x", got, tt.want)
			}
		})
	}
}

func repeatItem(item []byte, n int) [][]byte {
	items := make([][]byte, n)
	for i := range items {
		items[i] = item
	}
	return items
}
