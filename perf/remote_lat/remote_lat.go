package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"syscall"
	"time"

	"github.com/op/zenio"
	"github.com/op/zenio/perf"
)

var (
	debug          = flag.Bool("debug", true, "debug protocol")
	scheme         = flag.String("scheme", "sp+tcp", "transport scheme to use")
	host           = flag.String("host", "127.0.0.1:4242", "host:port to connect to")
	messageSize    = flag.Int("message-size", 1024, "message size to send")
	roundtripCount = flag.Int("roundtrip-count", 1, "number of roundtrips")
)

func main() {
	flag.Parse()

	conn, err := zenio.Dial(*scheme, *host, *debug)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	var frames [][]byte
	frames = append(frames, bytes.Repeat([]byte("o"), *messageSize))
	msg := zenio.NewBytesMessage(frames)

	watch := perf.NewStopWatch()

	for i := 0; i < *roundtripCount; i++ {
		msg.Reset()
		if err := conn.Send(msg); err != nil {
			op, ok := err.(*net.OpError)
			if ok {
				switch op.Err {
				case syscall.EPIPE:
					fmt.Printf("| broken pipe\n")
					return
				case syscall.ECONNRESET:
					fmt.Printf("| connection reset\n")
					return
				}
			}
			fmt.Printf("%#v\n", err)
			panic(err)
		}

		// rep
		if r, err := conn.Recv(); err != nil {
			if err != io.EOF {
				panic(err)
			}
		} else {
			for r.More() {
				frame, err := r.Next()
				if err != nil {
					panic(err)
				}
				if _, err = io.Copy(ioutil.Discard, frame); err != nil {
					panic(err)
				}
			}
		}
	}

	elapsed := watch.Stop() / time.Microsecond
	latency := float32(elapsed) / float32(*roundtripCount*2)

	fmt.Printf("message size: %d [B]\n", *messageSize)
	fmt.Printf("roundtrip count: %d\n", *roundtripCount)
	fmt.Printf("average latency: %.3f [us]\n", latency)
}
