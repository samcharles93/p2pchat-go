package main

import (
	"flag"
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"
)

// stringList is a custom flag type for multiple peer addresses
type stringList []string

func (s *stringList) String() string {
	return fmt.Sprintf("%v", *s)
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	var listenAddr string
	var peerAddrs stringList
	var disableDiscovery bool
	var useTUI bool
	var useGUI bool

	flag.StringVar(&listenAddr, "listen", ":0", "address to listen on (:0 = auto-assign port)")
	flag.Var(&peerAddrs, "peer", "peer address to connect to (can be specified multiple times)")
	flag.BoolVar(&disableDiscovery, "no-discovery", false, "disable auto-discovery")
	flag.BoolVar(&useTUI, "tui", false, "use beautiful TUI interface")
	flag.BoolVar(&useGUI, "gui", false, "use cross-platform GUI (not yet implemented)")
	flag.Parse()

	// Create enhanced node
	node, err := NewEnhancedNode(listenAddr, disableDiscovery)
	if err != nil {
		log.Fatalf("Failed to create enhanced node: %v", err)
	}

	// Connect to initial peers
	for _, addr := range peerAddrs {
		go node.connectToPeer(addr)
	}

	if useGUI {
		// Start with cross-platform GUI
		gui := NewChatGUI(node)

		// Start enhanced node in background
		go node.StartEnhanced()

		// Run GUI (this blocks until window is closed)
		gui.ShowAndRun()
	} else if useTUI {
		// Start with beautiful TUI (deprecated)
		ui := NewUI(node.Node)
		p := tea.NewProgram(ui, tea.WithAltScreen())

		// Start enhanced node in background
		go node.StartEnhanced()

		// Run TUI
		if _, err := p.Run(); err != nil {
			log.Fatalf("Error running TUI: %v", err)
		}
	} else {
		// Use legacy CLI
		node.StartEnhanced()
	}
}
