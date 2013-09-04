package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
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

	var msg zenio.BytesMessage
	frame := bytes.Repeat([]byte("o"), *messageSize)
	msg.AppendFrom(*messageSize, bytes.NewReader(frame))

	watch := perf.NewStopWatch()

	for i := 0; i < *roundtripCount; i++ {
		if err := conn.Send(&msg); err != nil {
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
		if err := conn.Recv(&msg); err != nil {
			if err != io.EOF {
				panic(err)
			}
		}
	}

	elapsed := watch.Stop() / time.Microsecond
	latency := float32(elapsed) / float32(*roundtripCount*2)

	fmt.Printf("message size: %d [B]\n", *messageSize)
	fmt.Printf("roundtrip count: %d\n", *roundtripCount)
	fmt.Printf("average latency: %.3f [us]\n", latency)
}
