package zmtp

import (
	"bytes"
	"encoding/binary"
	"io"
	"syscall"
	"testing"

	"github.com/op/zenio/protocol"
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
			0xff, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // length, 1<<63+1
			0x00, // flags
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
	testCases[5].ident = make([]byte, 0xff)
}

// byteMessage is a simple memory based Message implementation.
type byteMessage struct {
	frames [][]byte
	idx    int
}

func newByteMessage(frames [][]byte) *byteMessage {
	return &byteMessage{frames, 0}
}
func (m *byteMessage) More() bool {
	return m.idx < len(m.frames)
}

func (m *byteMessage) Next() (protocol.Frame, error) {
	if m.idx >= len(m.frames) {
		// TODO error
		panic("zenio: out of range")
	}

	frame := m.frames[m.idx]
	m.idx++
	return bytes.NewReader(frame), nil
}

func (m *byteMessage) rewind() bool {
	m.idx = 0
	return true
}

func TestReader(t *testing.T) {
	for i, c := range testCases {
		if c.skipDec {
			continue
		}
		var (
			j     int
			n     int64
			err   error
			msg   protocol.Message
			frame protocol.Frame
		)
		r := NewReader(bytes.NewReader(c.encoded))
		if msg, err = r.Read(); err != nil {
			t.Fatal(err)
		}
		for _, p := range c.decoded {
			frame, err = msg.Next()
			j++
			if err != nil {
				break
			} else if frame.Len() != len(p) {
				t.Errorf("%d invalid length", i)
			}

			var buf bytes.Buffer
			if n, err = io.Copy(&buf, frame); err != nil {
				break
			} else if n != int64(len(p)) {
				t.Errorf("%d invalid length", i)
			} else if !bytes.Equal(buf.Bytes(), p) {
				t.Errorf("%d %#v != %#v", i, buf, p)
			}

			if !msg.More() {
				break
			}
		}

		if err != c.err {
			t.Errorf("%d %#v != %#v", i, err, c.err)
		} else if msg.More() {
			t.Fatal("not all frames consumed")
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
			buf bytes.Buffer
			err error
		)
		w := NewWriter(&buf)
		w.Identity = c.ident

		msg := newByteMessage(c.decoded)
		err = w.Write(msg)
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
	zmtp := make([]byte, size+1+8+1)
	if size+1 < 0xff {
		zmtp[0] = uint8(size) + 1
	} else {
		zmtp[0] = 0xff
		binary.BigEndian.PutUint64(zmtp[1:], uint64(size+1))
	}

	zmtpr := bytes.NewReader(zmtp)
	r := NewReader(zmtpr)
	r.setupDone = true

	var buf bytes.Buffer
	buf.Grow(int(size))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		zmtpr.Seek(0, 0)
		buf.Reset()

		msg, err := r.Read()
		if err != nil {
			b.Fatal(err)
		}
		if frame, err := msg.Next(); err != nil {
			b.Fatal(err)
		} else {
			frameLen := frame.Len()
			if written, err := io.Copy(&buf, frame); err != nil {
				b.Fatal(err)
			} else if buf.Len() != frameLen || written != int64(buf.Len()) {
				b.Fatal("size")
			} else if msg.More() {
				b.Fatal("more")
			}
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

	expectedSize := int(size) + 2
	if size+1 >= 0xff {
		expectedSize += 8
	}

	var buf bytes.Buffer
	buf.Grow(expectedSize)
	w := NewWriter(&buf)
	w.setupDone = true

	p := make([]byte, size)
	msg := newByteMessage([][]byte{p})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		msg.rewind()
		if err := w.Write(msg); err != nil {
			b.Fatal(err)
		} else if buf.Len() != expectedSize {
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
