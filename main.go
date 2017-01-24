package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	tcpt "gx/ipfs/QmNiFRHdeJgN9svSGkEtmhd5sGvv3suSKZ1sRkLqoCzL76/go-tcp-transport"
	host "gx/ipfs/QmPsRtodRuBUir32nz5v4zuSBTSszrR1d3fA6Ahb6eaejj/go-libp2p-host"
	gologging "gx/ipfs/QmQvJiADDe7JR4m968MwXobTCCzUqQkP87aRHe29MEBGHV/go-logging"
	inet "gx/ipfs/QmQx1dHDDYENugYgqA22BaBrRfuv1coSsuPiM7rYh1wwGH/go-libp2p-net"
	net "gx/ipfs/QmQx1dHDDYENugYgqA22BaBrRfuv1coSsuPiM7rYh1wwGH/go-libp2p-net"
	msmux "gx/ipfs/QmRVYfZ7tWNHPBzWiG6KWGzvT2hcGems8srihsQE29x1U5/go-smux-multistream"
	golog "gx/ipfs/QmSpJByNKFX1sCsHBEp3R73FL4NF6FnQTEGyNAXHm2GS52/go-log"
	ma "gx/ipfs/QmUAQaWbKxGCUTuoQVvvicbQNZ9APF5pDGWyAZSe93AtKH/go-multiaddr"
	spdy "gx/ipfs/QmWUNsat6Jb19nC5CiJCDXepTkxjdxi3eZqeoB6mrmmaGu/go-smux-spdystream"
	testutil "gx/ipfs/QmbHpE8HYbHdwAxyiJMsPzywVyZKLLHNi5HemfVCPwo839/go-testutil"
	mplex "gx/ipfs/QmbPSoTDH3JFKkSxsCQDyVMFT76KcQz7ELWEetyyg3aeDA/go-smux-multiplex"
	swarm "gx/ipfs/Qmbiq2d2ZMi34A6V22kNY3b4GgPGFztmRCQZ931TJkYWp7/go-libp2p-swarm"
	yamux "gx/ipfs/Qmbn7RYyWzBVXiUp9jZ1dA4VADHy9DtS7iZLwfhEUQvm3U/go-smux-yamux"
	bhost "gx/ipfs/QmdzDdLZ7nj133QvNHypyS9Y39g35bMFk5DJ2pmX7YqtKU/go-libp2p/p2p/host/basic"
	peerstore "gx/ipfs/QmeXj9VAjmYQZxpmVz7VzccbJrpmr8qkCDSjfVNsPTWTYU/go-libp2p-peerstore"
	pstore "gx/ipfs/QmeXj9VAjmYQZxpmVz7VzccbJrpmr8qkCDSjfVNsPTWTYU/go-libp2p-peerstore"
	peer "gx/ipfs/QmfMmLGoKzCHDN7cGgk64PJr4iipzidDRME8HABSJqvmhC/go-libp2p-peer"
)

// create a 'Host' with a random peer to listen on the given address
func makeBasicHost(listen string, secio bool) (host.Host, error) {
	addr, err := ma.NewMultiaddr(listen)
	if err != nil {
		return nil, err
	}

	ps := pstore.NewPeerstore()
	var pid peer.ID

	if secio {
		ident, err := testutil.RandIdentity()
		if err != nil {
			return nil, err
		}

		ident.PrivateKey()
		ps.AddPrivKey(ident.ID(), ident.PrivateKey())
		ps.AddPubKey(ident.ID(), ident.PublicKey())
		pid = ident.ID()
	} else {
		fakepid, err := testutil.RandPeerID()
		if err != nil {
			return nil, err
		}
		pid = fakepid
	}

	ctx := context.Background()
	pstpt := msmux.NewBlankTransport()
	pstpt.AddTransport("/mplex/6.7.0", mplex.DefaultTransport)
	pstpt.AddTransport("/spdy/3.1.0", spdy.Transport)
	pstpt.AddTransport("/yamux/1.0.0", yamux.DefaultTransport)

	// create a new swarm to be used by the service host
	swrm, err := swarm.NewSwarmWithProtector(ctx, nil, pid, ps, nil, pstpt, nil)
	if err != nil {
		return nil, err
	}

	tcp := tcpt.NewTCPTransport()
	swrm.AddTransport(tcp)

	if err := swrm.Listen(addr); err != nil {
		return nil, err
	}

	log.Printf("I am %s/ipfs/%s\n", addr, pid.Pretty())
	return bhost.New((*swarm.Network)(swrm)), nil
}

func main() {
	golog.SetAllLoggers(gologging.INFO) // Change to DEBUG for extra info
	listenF := flag.Int("l", 0, "wait for incoming connections")
	target := flag.String("d", "", "target peer to dial")
	secio := flag.Bool("secio", false, "enable secio")

	flag.Parse()

	if *listenF == 0 {
		log.Fatal("Please provide a port to bind on with -l")
	}

	listenaddr := fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", *listenF)

	ha, err := makeBasicHost(listenaddr, *secio)
	if err != nil {
		log.Fatal(err)
	}

	// Set a stream handler on host A
	ha.SetStreamHandler("/echo/1.0.0", func(s net.Stream) {
		log.Println("Got a new stream!")
		defer s.Close()
		doEcho(s)
	})

	if *target == "" {
		log.Println("listening for connections")
		select {} // hang forever
	}
	// This is where the listener code ends

	ipfsaddr, err := ma.NewMultiaddr(*target)
	if err != nil {
		log.Fatalln(err)
	}

	pid, err := ipfsaddr.ValueForProtocol(ma.P_IPFS)
	if err != nil {
		log.Fatalln(err)
	}

	peerid, err := peer.IDB58Decode(pid)
	if err != nil {
		log.Fatalln(err)
	}

	tptaddr := strings.Split(ipfsaddr.String(), "/ipfs/")[0]
	// This creates a MA with the "/ip4/ipaddr/tcp/port" part of the target
	tptmaddr, err := ma.NewMultiaddr(tptaddr)
	if err != nil {
		log.Fatalln(err)
	}

	// We need to add the target to our peerstore, so we know how we can
	// contact it
	ha.Peerstore().AddAddr(peerid, tptmaddr, peerstore.PermanentAddrTTL)

	log.Println("opening stream")
	// make a new stream from host B to host A
	// it should be handled on host A by the handler we set above
	s, err := ha.NewStream(context.Background(), peerid, "/echo/1.0.0")
	if err != nil {
		log.Fatalln(err)
	}

	go func() {
		io.Copy(s, os.Stdin)
		s.Close()
	}()

	io.Copy(os.Stdout, s)
}

// doEcho reads some data from a stream, writes it back and closes the
// stream.
func doEcho(s inet.Stream) {
	for {
		buf := make([]byte, 1024)
		n, err := s.Read(buf)
		if err != nil {
			log.Println(err)
			return
		}

		log.Printf("read request: %q\n", buf[:n])
		_, err = s.Write(buf[:n])
		if err != nil {
			log.Println(err)
			return
		}
	}
}
