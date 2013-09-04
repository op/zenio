package zmtp

import (
	"bytes"
	"encoding/binary"
	"syscall"
	"testing"
)

var testCases = []struct {
	err     error
	ident   []byte
	encoded []byte
	decoded [][]byte
	skipDec bool
	skipEnc bool
}{
	{
		encoded: []byte{
			0x01, 0x00, // header, anonymous ident
			0x01, 0x00, // length and flags
		},
		decoded: [][]byte{
			{},
		},
	}, {
		encoded: []byte{
			0x04, 0x00, 0x01, 0x02, 0x03, // header, with ident
			0x01, 0x00, // length and flags
		},
		ident: []byte{0x01, 0x02, 0x03},
		decoded: [][]byte{
			{},
		},
	}, {
		encoded: []byte{
			0x01, 0x00, // header
			0x06, 0x00, // length and flags
			'h', 'e', 'l', 'l', 'o',
		},
		decoded: [][]byte{
			[]byte("hello"),
		},
	}, {
		encoded: []byte{
			0x07, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, // header
			0x03, 0x01, // length + more flag
			'p', '1',
			0x03, 0x00, // length
			'p', '2',
		},
		ident: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
		decoded: [][]byte{
			[]byte("p1"),
			[]byte("p2"),
		},
	}, {
		encoded: []byte{
			0x01, 0x00, // header
			0xff, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, // length
			0x00, // flags
			// 0xff bytes added in init()
		},
		decoded: [][]byte{
		// 0xff bytes added in init()
		},
	}, {
		err:     errIdentTooBig,
		skipDec: true,
		// ident added in init() to be have len >= 0xff
		decoded: [][]byte{{}},
	}, {
		err:     errIdentReserved,
		skipDec: true,
		ident:   []byte{0x00}, // illegal ident
		encoded: []byte{
			0x02, 0x00,
		},
		decoded: [][]byte{
			{},
		},
	}, {
		err:     syscall.EFBIG,
		skipEnc: true,
		encoded: []byte{
			0x01, 0x00, // header
			0xff, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x01, // length
			0x00, // flags
			// 0x100 bytes added in init(), which is > the buffer of
			// 0xff passed in when reading
		},
		decoded: [][]byte{
			{},
		},
	},
}

func init() {
	addBytesToTestCase := func(n int, size int) {
		tc := &testCases[n]
		tc.encoded = append(tc.encoded, make([]byte, size)...)
		tc.decoded = append(tc.decoded, make([]byte, size))
	}
	addBytesToTestCase(4, 0xff)
	addBytesToTestCase(7, 0x100)
	testCases[5].ident = make([]byte, 0xff)
}

func TestReader(t *testing.T) {
	for i, c := range testCases {
		if c.skipDec {
			continue
		}
		var (
			n   int
			err error
		)
		r := NewReader(bytes.NewReader(c.encoded))
		for i, p := range c.decoded {
			var buf [0xff]byte
			if n, err = r.Read(buf[:]); err != nil {
				break
			} else if n != len(p) {
				t.Errorf("%d invalid length", i)
			} else if !bytes.Equal(buf[:n], p) {
				t.Errorf("%d %#v != %#v", i, buf, p)
			}

			more := i+1 < len(c.decoded)
			if r.More() != more {
				t.Errorf("%d %v != %v", r.More(), more)
			}
		}

		if err != c.err {
			t.Errorf("%d %#v != %#v", i, err, c.err)
		}
	}
}

func TestWriter(t *testing.T) {
	// write one frame at a time
	for i, c := range testCases {
		if c.skipEnc {
			continue
		}
		var (
			n   int
			buf bytes.Buffer
			err error
		)
		w := NewWriter(&buf)
		w.Identity = c.ident
		for i, p := range c.decoded {
			w.SetMore(i+1 < len(c.decoded))
			if n, err = w.Write(p); err != nil {
				break
			} else if n != len(p) {
				t.Errorf("invalid length", i)
			}
		}

		if err != c.err {
			t.Errorf("%d %#v != %#v", i, err, c.err)
		} else if !bytes.Equal(buf.Bytes(), c.encoded) {
			t.Errorf("%d %#v != %#v", i, buf.Bytes(), c.encoded)
		}
	}
}

func benchmarkReader(b *testing.B, size int64) {
	b.SetBytes(size)

	// Setup the buffer differently depending on the size of the run. Also,
	// leave one extra byte for the frame flags.
	buf := make([]byte, size+1+8+1)
	if size+1 < 0xff {
		buf[0] = uint8(size) + 1
	} else {
		buf[0] = 0xff
		binary.BigEndian.PutUint64(buf[1:], uint64(size+1))
	}

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

func BenchmarkZMTPReader128(b *testing.B) {
	benchmarkReader(b, 128)
}

func BenchmarkZMTPReader1024k(b *testing.B) {
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

func BenchmarkZMTPWriter128(b *testing.B) {
	benchmarkWriter(b, 128)
}

func BenchmarkZMTPWriter1024k(b *testing.B) {
	benchmarkWriter(b, 1024*1024)
}
