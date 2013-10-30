package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"syscall"

	"github.com/op/zenio"
)

var (
	debug          = flag.Bool("debug", true, "debug protocol")
	scheme         = flag.String("scheme", "sp+tcp", "transport scheme to use")
	host           = flag.String("host", "127.0.0.1:4242", "host:port to bind to")
	messageSize    = flag.Int("message-size", 1024, "message size to receive")
	roundtripCount = flag.Int("roundtrip-count", 1, "number of roundtrips")
)

func main() {
	flag.Parse()

	ln, err := zenio.Listen(*scheme, *host, *debug)
	if err != nil {
		panic(err)
	}
	defer ln.Close()

	for i := 0; i < *roundtripCount; i++ {
		msg, err := ln.Recv()
		if err != nil {
			if err != io.EOF {
				panic(err)
			}
		}

		if err := ln.Send(msg); err != nil {
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
	}
}
