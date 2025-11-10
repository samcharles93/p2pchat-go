package main

import (
	"fmt"
	"log"
	"net"
)

func NewNode(listenAddr string, disableDiscovery bool) (*Node, error) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	// FIX: Get proper IPv4 address
	addr := listener.Addr().String()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		addr = fmt.Sprintf("127.0.0.1%s", port)
	} else if host == "::" {
		addr = fmt.Sprintf("127.0.0.1:%s", port)
	}

	// Initialize crypto manager
	cryptoManager, err := NewCryptoManager("./keys")
	if err != nil {
		log.Printf("Warning: Failed to initialize encryption: %v", err)
		log.Printf("Continuing without encryption")
	}

	node := &Node{
		ID:             addr,
		Listener:       listener,
		Peers:          make(map[string]*Peer),
		KnownPeers:     make(map[string]bool),
		IncomingMsg:    make(chan Message, 10),
		NewPeer:        make(chan *Peer),
		RemovePeer:     make(chan string),
		CLIInput:       make(chan string),
		Shutdown:       make(chan struct{}),
		DiscoveredPeer: make(chan string, 10),
		PeerListGossip: make(chan []string, 10),
		uiChannel:      make(chan Message, 100), // Buffer for UI messages
		cryptoManager:  cryptoManager,
	}

	node.KnownPeers[node.ID] = true

	// Setup UDP multicast for discovery
	if !disableDiscovery {
		mcastAddr, err := net.ResolveUDPAddr("udp", multicastAddr)
		if err != nil {
			log.Printf("Warning: Failed to resolve multicast addr %s: %v", multicastAddr, err)
			log.Printf("Continuing without auto-discovery. Use /connect <addr> to add peers manually.")
			return node, nil
		}

		conn, err := net.ListenMulticastUDP("udp", nil, mcastAddr)
		if err != nil {
			log.Printf("Warning: Failed to join multicast group %s: %v", multicastAddr, err)
			log.Printf("Continuing without auto-discovery. Use /connect <addr> to add peers manually.")
			return node, nil
		}

		node.discoveryConn = conn
		log.Printf("Auto-discovery enabled on %s", multicastAddr)
	} else {
		log.Printf("Auto-discovery disabled")
	}

	return node, nil
}

func (n *Node) Start() {
	log.Printf("Node listening on %s (ID: %s)", n.Listener.Addr(), n.ID)
	fmt.Println("Commands: /quit to exit, /connect <addr> to add peer, /peers to list peers, /discovered to list discovered peers")

	// Start goroutines
	n.wg.Add(1)
	go n.handleServer()

	n.wg.Add(1)
	go n.handleCLI()

	if n.discoveryConn != nil {
		n.wg.Add(1)
		go n.handleDiscovery()

		n.wg.Add(1)
		go n.announcePresence()

		n.wg.Add(1)
		go n.gossipPeerList()
	}

	n.eventLoop()
}

func (n *Node) eventLoop() {
	for {
		select {
		case peer := <-n.NewPeer:
			n.addPeer(peer)

		case peerID := <-n.RemovePeer:
			n.removePeer(peerID)

		case msg := <-n.IncomingMsg:
			n.handleIncomingMessage(msg)

		case input := <-n.CLIInput:
			n.handleCLIInput(input)

		case peerAddr := <-n.DiscoveredPeer:
			n.handleDiscoveredPeer(peerAddr)

		case peerList := <-n.PeerListGossip:
			n.handlePeerListGossip(peerList)

		case <-n.Shutdown:
			n.shutdown()
			return
		}
	}
}

func (n *Node) shutdown() {
	n.shutdownOnce.Do(func() {
		close(n.Shutdown)
		n.Listener.Close()
		if n.discoveryConn != nil {
			n.discoveryConn.Close()
		}

		n.peersMutex.Lock()
		for _, peer := range n.Peers {
			peer.once.Do(func() {
				close(peer.Done)
			})
		}
		n.peersMutex.Unlock()

		n.wg.Wait()
		log.Println("Node shut down")
	})
}
