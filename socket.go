// Copyright 2013 The Zenio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// XXX this file is a mess and is just a proof of concept and a brain dump.

package zenio

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/op/zenio/protocol/sp"
	"github.com/op/zenio/protocol/zmtp"
)

type protocol int

const (
	protoSP protocol = iota
	protoZMTP
)

type protocolReader interface {
	io.Reader
	Init() error
	Peek() error
	Len() int
	More() bool
}

type protocolWriter interface {
	io.Writer
	Init() error
	SetMore(bool)
}

// TODO add options for specific socket types. eg keep-alives for tcp etc.
// TODO implement inproc using net.Pipe()? channels?

type Identity []byte

type msgErrCh struct {
	msg  Message
	errc chan error
}

// TODO add new type which contains multiple sockets
// TODO split into two separate types (listen / conn)
// TODO implement net.Conn
// TODO implement net.Listener
type socket struct {
	recv  chan msgErrCh
	send  chan msgErrCh
	peers []*peer
	ident Identity
	ln    net.Listener
	debug bool
	dmu   sync.Mutex

	network  string
	address  string
	protocol protocol
}

func newSocket(debug bool) *socket {
	return &socket{
		recv:  make(chan msgErrCh),
		send:  make(chan msgErrCh),
		debug: debug,
	}
}

func (s *socket) init(network string, address string) error {
	var proto protocol
	protoNet := strings.SplitN(network, "+", 2)
	if len(protoNet) == 1 {
		proto = protoSP
	} else {
		switch protoNet[0] {
		case "sp":
			proto = protoSP
		case "zmtp":
			proto = protoZMTP
		default:
			return errors.New("zenio: unsupported protocol: " + protoNet[0])
		}
		network = protoNet[1]
	}
	s.network = network
	s.address = address
	s.protocol = proto

	switch s.network {
	case "tcp":
		// supported
	case "inproc":
		panic("zmq: not implemented: inproc")
	case "ipc":
		panic("zmq: not implemented: ipc")
	default:
		panic("unknown scheme")
	}

	return nil
}

func (s *socket) connect(network, address string) error {
	if err := s.init(network, address); err != nil {
		return err
	}

	// TODO keep track of already connected peers
	peer, err := newConnPeer(s, s.network, s.address)
	if err != nil {
		return err
	}

	s.peers = append(s.peers, peer)
	return nil
}

func (s *socket) listen(network, address string) error {
	if err := s.init(network, address); err != nil {
		return err
	}

	// TODO do non-blocking listen?
	ln, err := net.Listen(s.network, s.address)
	if err != nil {
		return err
	}

	s.ln = ln
	go s.listener()
	return nil
}

func (s *socket) listener() {
	connected := make(chan *peer)

	// registers peers
	go func() {
		for {
			peer := <-connected
			s.peers = append(s.peers, peer)

			if s.debug {
				fmt.Printf("\nAccepted %s (%s) <-> %s (%s)..\n",
					peer.conn.LocalAddr(), s.ident,
					peer.conn.RemoteAddr(), peer.ident)
			}
		}
	}()

	for {
		// accepts new connections
		conn, err := s.ln.Accept()
		if err != nil {
			panic(err)
		}

		// handshakes with connection
		go func() {
			peer, err := newListenPeer(s, conn)
			if err != nil {
				panic(err)
			}
			connected <- peer
		}()

	}
}

func (s *socket) Close() error {
	var err error
	// TODO filter errors
	for _, peer := range s.peers {
		if e := peer.Close(); e != nil {
			err = e
		}
	}
	return err
}

func (s *socket) Recv(msg Message) error {
	errc := make(chan error, 1)
	s.recv <- msgErrCh{msg, errc}
	return <-errc
}

func (s *socket) Send(msg Message) error {
	// TODO route message based on identity
	errc := make(chan error, 1)
	s.send <- msgErrCh{msg, errc}
	return <-errc
}

type peer struct {
	sock *socket

	ident Identity
	conn  net.Conn

	network string
	address string
	// dialer  net.Dialer
}

func newConnPeer(sock *socket, network, address string) (*peer, error) {
	peer := &peer{
		sock:    sock,
		network: network,
		address: address,
	}
	// TODO move connection logic into sender / receiver
	// TODO do non-blocking connect?
	if err := peer.connect(); err != nil {
		return nil, err
	}
	go peer.sender()
	go peer.receiver()

	return peer, nil
}

func newListenPeer(sock *socket, conn net.Conn) (*peer, error) {
	peer := &peer{
		sock: sock,
		conn: conn,
	}
	go peer.sender()
	go peer.receiver()

	return peer, nil
}

func (p *peer) connect() error {
	var dialer net.Dialer
	var backoff = backoff{Max: 10 * time.Second}
	for {
		dialer.Deadline = time.Now().Add(3 * time.Second)

		if p.sock.debug {
			println("Connecting", p.network, p.address, dialer.Deadline.String())
		}
		conn, err := dialer.Dial(p.network, p.address)
		if err != nil {
			backoff.Wait()
			continue
		}

		p.conn = conn
		if p.sock.debug {
			fmt.Printf("\nConnected %s (%s) <-> %s (%s)..\n",
				conn.LocalAddr(), p.sock.ident,
				conn.RemoteAddr(), p.ident)
		}
		break
	}
	return nil
}

func (p *peer) sender() {
	var dumper = hexDumper{Prefix: ">> "}

	var w io.Writer = p.conn
	if p.sock.debug {
		w = io.MultiWriter(w, &dumper)
	}
	buf := bufio.NewWriter(w)

	var proto protocolWriter
	switch p.sock.protocol {
	case protoSP:
		proto = sp.NewWriter(buf)
	case protoZMTP:
		proto = zmtp.NewWriter(buf)
	default:
		panic("unhandled protocol")
	}

	if err := proto.Init(); err != nil {
		panic(err)
	} else if err := buf.Flush(); err != nil {
		panic(err)
	}

	// TODO detect when we can't send
	for {
		send := <-p.sock.send

		var err error
		for i := 0; i < send.msg.Len(); i++ {
			proto.SetMore(i+1 < send.msg.Len())
			if _, err = proto.Write(send.msg.Bytes(i)); err != nil {
				break
			}
		}
		if err == nil {
			err = buf.Flush()
		}
		send.errc <- err
		if p.sock.debug {
			p.sock.dmu.Lock()
			dumper.Flush()
			p.sock.dmu.Unlock()
		}
	}
}

func (p *peer) receiver() {
	var dumper = hexDumper{Prefix: "<< "}

	var reader io.Reader = bufio.NewReader(p.conn)
	if p.sock.debug {
		reader = io.TeeReader(reader, &dumper)
	}

	var proto protocolReader
	switch p.sock.protocol {
	case protoSP:
		proto = sp.NewReader(reader)
	case protoZMTP:
		proto = zmtp.NewReader(reader)
	default:
		panic("unhandled protocol")
	}

	if err := proto.Init(); err != nil {
		panic(err)
	}

	// TODO detect when we can't receive and reconnect
	for {
		recv := <-p.sock.recv
		recv.msg.Reset()

		var err error
		for {
			if err = proto.Peek(); err != nil {
				break
			}
			err = recv.msg.AppendFrom(proto.Len(), proto)
			if !proto.More() {
				break
			}
		}
		recv.errc <- err

		if p.sock.debug {
			p.sock.dmu.Lock()
			dumper.Flush()
			p.sock.dmu.Unlock()
		}
	}
}

func (p *peer) Close() error {
	return p.conn.Close()
}

// Dial
func Dial(scheme, network string, debug bool) (*socket, error) {
	var d dialer
	return d.Dial(scheme, network, debug)
}

// A Dialer contains options for connecting to addresses.
// TODO refactor. move into conn?
type dialer struct {
	// LocalAddr is the local address to use when dialing an address. The
	// address must be of a compatible type for the network being dialed.
	//
	// If nil, a local address is automatically chosen.
	// TODO
	// LocalAddr net.Addr

	// TODO add deadlines and other knobs.
}

func (d *dialer) Dial(network, address string, debug bool) (*socket, error) {
	sock := newSocket(debug)
	return sock, sock.connect(network, address)
}

func Listen(network, address string, debug bool) (*socket, error) {
	sock := newSocket(debug)
	return sock, sock.listen(network, address)
}
