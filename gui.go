package main

import (
	"log"
)

type ChatGUI struct {
	node *EnhancedNode
}

func NewChatGUI(node *EnhancedNode) *ChatGUI {
	log.Println("GUI mode not yet fully implemented")
	return &ChatGUI{node: node}
}

func (gui *ChatGUI) ShowAndRun() {
	log.Println("Running in CLI mode instead")
	gui.node.StartEnhanced()
}

func NewNodeWithGUI(listenAddr string, disableDiscovery bool) (*EnhancedNode, error) {
	return NewEnhancedNode(listenAddr, disableDiscovery)
}
