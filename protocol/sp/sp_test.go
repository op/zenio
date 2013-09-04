package sp

import (
	"bytes"
	"encoding/binary"
	"testing"
)

var testCases = []struct {
	err     error
	encoded []byte
	decoded [][]byte
}{
	{
		err: nil,
		encoded: []byte{
			0x00, 0x53, 0x50, 0x00, 0x00, 0x10, 0x00, 0x00, // header
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // length
		},
		decoded: [][]byte{
			[]byte{},
		},
	}, {
		err: nil,
		encoded: []byte{
			0x00, 0x53, 0x50, 0x00, 0x00, 0x10, 0x00, 0x00, // header
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, // length
			'h', 'e', 'l', 'l', 'o',
		},
		decoded: [][]byte{
			[]byte("hello"),
		},
	}, {
		err: nil,
		encoded: []byte{
			0x00, 0x53, 0x50, 0x00, 0x00, 0x10, 0x00, 0x00, // header
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // length
			'p', '1',
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // length
			'p', '2',
		},
		decoded: [][]byte{
			[]byte("p1"),
			[]byte("p2"),
		},
	},
}

func TestReader(t *testing.T) {
	for _, c := range testCases {
		var (
			n   int
			err error
		)
		r := NewReader(bytes.NewReader(c.encoded))
		for _, p := range c.decoded {
			var buf [128]byte
			if n, err = r.Read(buf[:]); err != nil {
				break
			} else if n != len(p) {
				t.Error("invalid length")
			} else if !bytes.Equal(buf[:n], p) {
				t.Errorf("%#v != %#v", buf, p)
			}
		}

		if err != c.err {
			t.Errorf("%#v != %#v", err, c.err)
		}
	}
}

func TestWriter(t *testing.T) {
	for _, c := range testCases {
		var (
			n   int
			buf bytes.Buffer
			err error
		)
		w := NewWriter(&buf)
		for _, p := range c.decoded {
			if n, err = w.Write(p); err != nil {
				break
			} else if n != len(p) {
				t.Error("invalid length")
			}
		}

		if err != c.err {
			t.Errorf("%#v != %#v", err, c.err)
		}
		if !bytes.Equal(buf.Bytes(), c.encoded) {
			t.Errorf("%#v != %#v", buf.Bytes(), c.encoded)
		}
	}
}

func benchmarkReader(b *testing.B, size int64) {
	b.SetBytes(size)

	buf := make([]byte, size+8)
	binary.BigEndian.PutUint64(buf[:], uint64(size))
	br := bytes.NewReader(buf)
	r := NewReader(br)
	r.setupDone = true
	p := make([]byte, size)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.Seek(0, 0)
		if n, err := r.Read(p); err != nil {
			b.Fatal(err)
		} else if n != len(p) {
			b.Fatal("size")
		}
	}
}

func BenchmarkSPReader(b *testing.B) {
	benchmarkReader(b, 128)
}

func BenchmarkSPReader1024k(b *testing.B) {
	benchmarkReader(b, 1024*1024)
}

func benchmarkWriter(b *testing.B, size int64) {
	b.SetBytes(size)

	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.setupDone = true
	p := make([]byte, size)
	buf.Grow(int(size) * 2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if n, err := w.Write(p); err != nil {
			b.Fatal(err)
		} else if n != len(p) {
			b.Fatal("size")
		}
	}
}

func BenchmarkSPWriter128(b *testing.B) {
	benchmarkWriter(b, 128)
}

func BenchmarkSPWriter1024k(b *testing.B) {
	benchmarkWriter(b, 1024*1024)
}
