package sp

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/op/zenio/protocol"
)

func TestNegotiator(t *testing.T) {
	// TODO improve test
	var n Negotiator
	var buf bytes.Buffer
	var header = []byte{0x00, 0x53, 0x50, 0x00, 0x00, 0x10, 0x00, 0x00}
	_, _, err := n.Upgrade(bytes.NewReader(header), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), header) {
		t.Errorf("%#v != %#v", buf.Bytes(), header)
	}
}

var testCases = []struct {
	err     error
	encoded []byte
	decoded [][]byte
}{
	{
		err: nil,
		encoded: []byte{
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // length
		},
		decoded: [][]byte{
			[]byte{},
		},
	}, {
		err: nil,
		encoded: []byte{
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, // length
			'h', 'e', 'l', 'l', 'o',
		},
		decoded: [][]byte{
			[]byte("hello"),
		},
	}, {
		err: nil,
		encoded: []byte{
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

// byteFrame is a memory based implementation of Message and Frame.
type byteFrame struct {
	frame []byte
	r     *bytes.Reader
}

func newByteMessage(frame []byte) *byteFrame {
	return &byteFrame{frame: frame}
}

func (b *byteFrame) More() bool {
	return b.r == nil
}

func (b *byteFrame) Next() (protocol.Frame, error) {
	if b.r != nil {
		// TODO error
		panic("out of range")
	}
	b.r = bytes.NewReader(b.frame)
	return b.r, nil
}

func (b *byteFrame) rewind() bool {
	b.r = nil
	return true
}

func TestReader(t *testing.T) {
	var (
		err   error
		msg   protocol.Message
		frame protocol.Frame
	)
	for _, c := range testCases {
		r := newReader(bytes.NewReader(c.encoded))
		for _, p := range c.decoded {
			var buf bytes.Buffer
			msg, err = r.Read()
			if err != nil {
				break
			}
			frame, err = msg.Next()
			if err != nil {
				break
			} else if frame.Len() != len(p) {
				t.Error("invalid length")
			}
			if n, err := io.Copy(&buf, frame); err != nil {
				break
			} else if n != int64(len(p)) {
				t.Error("invalid length")
			} else if !bytes.Equal(buf.Bytes(), p) {
				t.Errorf("%#v != %#v", buf, p)
			}

			if msg.More() {
				t.Fatal("more")
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
			buf bytes.Buffer
			err error
		)
		w := newWriter(&buf)
		for _, p := range c.decoded {
			msg := newByteMessage(p)
			if err = w.Write(msg); err != nil {
				break
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

	sp := make([]byte, size+8)
	binary.BigEndian.PutUint64(sp[:], uint64(size))
	spr := bytes.NewReader(sp)

	r := newReader(spr)

	var buf bytes.Buffer
	buf.Grow(int(size))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		spr.Seek(0, 0)
		buf.Reset()
		if msg, err := r.Read(); err != nil {
			b.Fatal(err)
		} else {
			frame, err := msg.Next()
			if err != nil {
				b.Fatal(err)
			} else if n, err := io.Copy(&buf, frame); err != nil {
				b.Fatal(err)
			} else if n != size {
				b.Fatal("size")
			}
			if msg.More() {
				b.Fatal("more")
			}
		}
	}
}

func BenchmarkSPReader128(b *testing.B) {
	benchmarkReader(b, 128)
}

func BenchmarkSPReader1024k(b *testing.B) {
	benchmarkReader(b, 1024*1024)
}

func benchmarkWriter(b *testing.B, size int64) {
	b.SetBytes(size)

	var buf bytes.Buffer
	w := newWriter(&buf)
	p := make([]byte, size)
	buf.Grow(int(size) * 2)

	msg := newByteMessage(p)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		msg.rewind()
		if err := w.Write(msg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSPWriter128(b *testing.B) {
	benchmarkWriter(b, 128)
}

func BenchmarkSPWriter1024k(b *testing.B) {
	benchmarkWriter(b, 1024*1024)
}
