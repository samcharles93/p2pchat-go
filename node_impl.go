package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

// Node methods implementation

func (n *Node) connectToPeer(addr string) {
	if addr == n.ID || addr == "" {
		log.Printf("Cannot connect to self or empty address")
		return
	}

	n.peersMutex.RLock()
	_, exists := n.Peers[addr]
	n.peersMutex.RUnlock()

	if exists {
		log.Printf("Already connected to %s", addr)
		return
	}

	log.Printf("Connecting to %s...", addr)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		log.Printf("Failed to connect to %s: %v", addr, err)
		return
	}

	log.Printf("Connected to %s", addr)
	peer := &Peer{
		ID:   addr,
		Conn: conn,
		Send: make(chan []byte, 10),
		Done: make(chan struct{}),
	}

	n.NewPeer <- peer
}

func (n *Node) addPeer(peer *Peer) {
	n.peersMutex.Lock()
	defer n.peersMutex.Unlock()

	if _, exists := n.Peers[peer.ID]; exists {
		log.Printf("Peer %s already exists, closing connection", peer.ID)
		peer.Conn.Close()
		return
	}

	n.Peers[peer.ID] = peer
	n.knownMutex.Lock()
	n.KnownPeers[peer.ID] = true
	n.knownMutex.Unlock()

	// Send to UI if available
	if n.uiChannel != nil {
		n.uiChannel <- Message{
			SenderID: "System",
			Content:  []byte(fmt.Sprintf("🔗 Peer connected: %s", peer.ID)),
		}
	}

	n.wg.Add(1)
	go n.handlePeer(peer)
}

func (n *Node) removePeer(peerID string) {
	n.peersMutex.Lock()
	defer n.peersMutex.Unlock()

	peer, exists := n.Peers[peerID]
	if !exists {
		return
	}

	delete(n.Peers, peerID)
	peer.once.Do(func() {
		close(peer.Done)
	})

	// Send to UI if available
	if n.uiChannel != nil {
		n.uiChannel <- Message{
			SenderID: "System",
			Content:  []byte(fmt.Sprintf("❌ Peer disconnected: %s", peerID)),
		}
	}
}

func (n *Node) handlePeer(peer *Peer) {
	defer n.wg.Done()

	n.wg.Add(1)
	go n.readPeer(peer)

	n.wg.Add(1)
	go n.writePeer(peer)

	<-peer.Done

	// Cleanup
	peer.once.Do(func() {
		close(peer.Send)
	})
	peer.Conn.Close()
	n.RemovePeer <- peer.ID
}

func (n *Node) readPeer(peer *Peer) {
	defer n.wg.Done()

	scanner := bufio.NewScanner(peer.Conn)
	for scanner.Scan() {
		line := scanner.Text()

		parts := strings.SplitN(line, string(delimiter), 2)
		if len(parts) != 2 {
			log.Printf("Invalid message format from %s: %s", peer.ID, line)
			continue
		}

		senderID := parts[0]
		content := parts[1]

		msg := Message{
			SenderID:   senderID,
			Content:    []byte(content),
			FromPeerID: peer.ID,
		}
		n.IncomingMsg <- msg

		// Also send to UI
		if n.uiChannel != nil {
			n.uiChannel <- msg
		}
	}

	if err := scanner.Err(); err != nil {
		select {
		case <-n.Shutdown:
			return
		default:
			log.Printf("Read error from %s: %v", peer.ID, err)
		}
	}

	peer.once.Do(func() {
		close(peer.Done)
	})
}

func (n *Node) writePeer(peer *Peer) {
	defer n.wg.Done()

	for data := range peer.Send {
		_, err := peer.Conn.Write(append(data, '\n'))
		if err != nil {
			select {
			case <-n.Shutdown:
				return
			default:
				log.Printf("Write error to %s: %v", peer.ID, err)
				peer.once.Do(func() {
					close(peer.Done)
				})
				return
			}
		}
	}
}

func (n *Node) handleServer() {
	defer n.wg.Done()

	for {
		conn, err := n.Listener.Accept()
		if err != nil {
			select {
			case <-n.Shutdown:
				return
			default:
				log.Printf("Accept error: %v", err)
				continue
			}
		}

		remoteAddr := conn.RemoteAddr().String()

		n.peersMutex.RLock()
		_, exists := n.Peers[remoteAddr]
		n.peersMutex.RUnlock()

		if exists {
			log.Printf("Already connected to %s, closing new connection", remoteAddr)
			conn.Close()
			continue
		}

		peer := &Peer{
			ID:   remoteAddr,
			Conn: conn,
			Send: make(chan []byte, 10),
			Done: make(chan struct{}),
		}

		n.NewPeer <- peer
	}
}

func (n *Node) handleCLI() {
	defer n.wg.Done()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")

	for scanner.Scan() {
		input := scanner.Text()
		n.CLIInput <- input
		fmt.Print("> ")
	}

	if err := scanner.Err(); err != nil {
		select {
		case <-n.Shutdown:
			return
		default:
			log.Printf("CLI read error: %v", err)
		}
	}

	// Use shutdown() method to safely close the channel
	n.shutdown()
}

func (n *Node) gossipPeerList() {
	defer n.wg.Done()

	ticker := time.NewTicker(gossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			n.sendPeerListGossip()
		case <-n.Shutdown:
			return
		}
	}
}

func (n *Node) handleCLIInput(input string) {
	switch {
	case input == "/quit":
		// Call shutdown() which handles channel close safely via sync.Once
		n.shutdown()

	case strings.HasPrefix(input, "/connect "):
		addr := strings.TrimPrefix(input, "/connect ")
		go n.connectToPeer(addr)

	case input == "/peers":
		n.listPeers()

	case input == "/discovered":
		n.listDiscoveredPeers()

	case input == "/help":
		n.showHelp()

	default:
		// Send as regular message
		msg := Message{
			SenderID:   n.ID,
			Content:    []byte(input),
			FromPeerID: "",
		}
		n.broadcast(msg)

		// Also send to UI
		if n.uiChannel != nil {
			n.uiChannel <- msg
		}
	}
}

func (n *Node) showHelp() {
	helpText := `Available Commands:
  /connect <addr> - Connect to peer
  /peers - List connected peers
  /discovered - List discovered peers
  /help - Show this help
  /quit - Exit application
`
	if n.uiChannel != nil {
		n.uiChannel <- Message{
			SenderID: "System",
			Content:  []byte(helpText),
		}
	} else {
		fmt.Println(helpText)
	}
}

func (n *Node) handleDiscoveredPeer(peerAddr string) {
	if peerAddr == n.ID {
		return
	}

	n.peersMutex.RLock()
	_, exists := n.Peers[peerAddr]
	n.peersMutex.RUnlock()

	if exists {
		return
	}

	n.knownMutex.RLock()
	recentlySeen := n.KnownPeers[peerAddr]
	n.knownMutex.RUnlock()

	if recentlySeen {
		return
	}

	log.Printf("Auto-discovered peer: %s", peerAddr)

	// Send to UI
	if n.uiChannel != nil {
		n.uiChannel <- Message{
			SenderID: "System",
			Content:  []byte(fmt.Sprintf("🔍 Auto-discovered peer: %s", peerAddr)),
		}
	}

	n.connectToPeer(peerAddr)
}

func (n *Node) handlePeerListGossip(peerList []string) {
	for _, peerAddr := range peerList {
		if peerAddr != "" && peerAddr != n.ID {
			n.DiscoveredPeer <- peerAddr
		}
	}
}

func (n *Node) listPeers() {
	n.peersMutex.RLock()
	defer n.peersMutex.RUnlock()

	if len(n.Peers) == 0 {
		fmt.Println("No connected peers")
		return
	}

	fmt.Println("Connected peers:")
	for id := range n.Peers {
		fmt.Printf("  - %s\n", id)
	}
}

func (n *Node) listDiscoveredPeers() {
	n.knownMutex.RLock()
	defer n.knownMutex.RUnlock()

	fmt.Println("All discovered peers:")
	for peer := range n.KnownPeers {
		status := "disconnected"

		// Need to acquire peersMutex to safely read n.Peers map
		n.peersMutex.RLock()
		_, connected := n.Peers[peer]
		n.peersMutex.RUnlock()

		if connected {
			status = "connected"
		}
		fmt.Printf("  - %s [%s]\n", peer, status)
	}
}
