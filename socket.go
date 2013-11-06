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
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/op/zenio/protocol"
	"github.com/op/zenio/protocol/sp"
	"github.com/op/zenio/protocol/zmtp"
)

// TODO add options for specific socket types. eg keep-alives for tcp etc.
// TODO implement inproc using net.Pipe()? channels?

var (
	DefaultSP   = &sp.Negotiator{}
	DefaultZMTP = &zmtp.Negotiator{}
)

type Identity []byte

type msgErrCh struct {
	msg  protocol.Message
	errc chan error
}

type msgErr struct {
	msg protocol.Message
	err error
}

// TODO add new type which contains multiple sockets
// TODO split into two separate types (listen / conn)
// TODO implement net.Conn
// TODO implement net.Listener
type socket struct {
	recv  chan chan *msgErr
	send  chan msgErrCh
	peers []*peer
	ident Identity
	ln    net.Listener
	debug bool
	dmu   sync.Mutex

	network  string
	address  string
	protocol string

	neg protocol.Negotiator
}

func newSocket(debug bool) *socket {
	return &socket{
		recv:  make(chan chan *msgErr),
		send:  make(chan msgErrCh),
		debug: debug,
	}
}

func (s *socket) init(network string, address string) error {
	var proto = "sp"
	protoNet := strings.SplitN(network, "+", 2)
	if len(protoNet) == 2 {
		proto = protoNet[0]
		network = protoNet[1]
	}
	s.network = network
	s.address = address
	s.protocol = proto

	switch s.protocol {
	case "sp":
		s.neg = DefaultSP
	case "zmtp":
		s.neg = DefaultZMTP
	default:
		panic("unhandled protocol: " + s.protocol)
	}

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

func (s *socket) Recv() (protocol.Message, error) {
	ch := make(chan *msgErr, 1)
	s.recv <- ch
	r := <-ch
	return r.msg, r.err
}

func (s *socket) Send(msg protocol.Message) error {
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

type flusher interface {
	Flush() error
}

type reader struct {
	pr protocol.Reader
	br *bufio.Reader
	hd *hexDumper
}

type writer struct {
	pw protocol.Writer
	bw *bufio.Writer
	hd *hexDumper
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
	r, w, err := peer.handshake()
	if err != nil {
		return nil, err
	}
	go peer.sender(w)
	go peer.receiver(r)

	return peer, nil
}

func newListenPeer(sock *socket, conn net.Conn) (*peer, error) {
	peer := &peer{
		sock: sock,
		conn: conn,
	}
	r, w, err := peer.handshake()
	if err != nil {
		return nil, err
	}
	go peer.sender(w)
	go peer.receiver(r)

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

func (p *peer) handshake() (*reader, *writer, error) {
	var dr, dw *hexDumper
	var w io.Writer = p.conn
	var r io.Reader = p.conn

	br := bufio.NewReader(r)
	r = br

	if p.sock.debug {
		dw = &hexDumper{Prefix: "» "}
		w = io.MultiWriter(w, dw)
		dr = &hexDumper{Prefix: "« "}
		r = io.TeeReader(r, dr)
	}

	bw := bufio.NewWriter(w)

	pr, pw, err := p.sock.neg.Upgrade(br, bw)
	if err != nil {
		return nil, nil, err
	}
	return &reader{pr, br, dr}, &writer{pw, bw, dw}, err
}

func (p *peer) sender(w *writer) {
	if w.hd != nil {
		w.hd.Flush()
	}

	// TODO detect when we can't send
	for {
		send := <-p.sock.send
		err := w.pw.Write(send.msg)
		if err == nil {
			err = w.bw.Flush()
		}
		if w.hd != nil {
			w.hd.Flush()
		}
		send.errc <- err
	}
}

func (p *peer) receiver(r *reader) {
	if r.hd != nil {
		r.hd.Flush()
	}

	pipeline := newPipelinedReader(r.pr)

	// TODO detect when we can't receive and reconnect
	for {
		ch := <-p.sock.recv

		// TODO allow timeout to happen when no reader can be fetched in time
		pipe := <-pipeline.reader()

		msg, err := pipe.Read()
		if r.hd != nil {
			r.hd.Flush()
		}
		ch <- &msgErr{msg, err}
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
