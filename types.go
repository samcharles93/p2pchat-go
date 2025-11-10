package main

import (
	"net"
	"sync"
)

const (
	multicastAddr  = "239.255.255.250:9999"
	delimiter      = '|'
	gossipInterval = 10 * 1000000000 // 10 seconds in nanoseconds
)

type Node struct {
	ID             string
	Listener       net.Listener
	Peers          map[string]*Peer
	peersMutex     sync.RWMutex
	KnownPeers     map[string]bool
	knownMutex     sync.RWMutex
	IncomingMsg    chan Message
	NewPeer        chan *Peer
	RemovePeer     chan string
	CLIInput       chan string
	Shutdown       chan struct{}
	shutdownOnce   sync.Once
	wg             sync.WaitGroup
	discoveryConn  *net.UDPConn
	DiscoveredPeer chan string
	PeerListGossip chan []string
	uiChannel      chan Message
	cryptoManager  *CryptoManager
}

type Peer struct {
	ID   string
	Conn net.Conn
	Send chan []byte
	Done chan struct{}
	once sync.Once
}

type Message struct {
	SenderID   string
	Content    []byte
	FromPeerID string
	IsGossip   bool
}
