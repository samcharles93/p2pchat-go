package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles for the TUI
var (
	// Color scheme
	primaryColor   = lipgloss.Color("#7C3AED") // Purple
	accentColor    = lipgloss.Color("#10B981") // Green
	warningColor   = lipgloss.Color("#F59E0B") // Amber
	errorColor     = lipgloss.Color("#EF4444") // Red
	mutedColor     = lipgloss.Color("#6B7280") // Gray
	backgroundColor = lipgloss.Color("#1F2937") // Dark gray

	// Component styles
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 1)

	peerPanelStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(mutedColor).
			Padding(0, 1)

	messagePanelStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(mutedColor).
				Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Background(backgroundColor).
			Padding(0, 1)

	inputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Padding(0, 1)

	// Message styles
	systemMessageStyle = lipgloss.NewStyle().
				Foreground(accentColor).
				Italic(true)

	userMessageStyle = lipgloss.NewStyle().
				Foreground(primaryColor).
				Bold(true)

	peerMessageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3B82F6"))

	timestampStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Faint(true)

	// Peer status styles
	peerConnectedStyle = lipgloss.NewStyle().
				Foreground(accentColor)

	peerDisconnectedStyle = lipgloss.NewStyle().
				Foreground(errorColor)
)

// Message represents a chat message with timestamp
type ChatMessage struct {
	Sender    string
	Content   string
	Timestamp time.Time
	IsSystem  bool
}

// UI represents the TUI model
type UI struct {
	node         *Node
	messages     []ChatMessage
	peers        []string
	viewport     viewport.Model
	textarea     textarea.Model
	ready        bool
	width        int
	height       int
	lastUpdate   time.Time
	showHelp     bool
}

// tickMsg is sent periodically to update the UI
type tickMsg time.Time

// messageMsg wraps incoming messages
type messageMsg Message

// NewUI creates a new TUI instance
func NewUI(node *Node) *UI {
	ta := textarea.New()
	ta.Placeholder = "Type a message or /help for commands..."
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 500
	ta.SetWidth(80)
	ta.SetHeight(1)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.ShowLineNumbers = false

	vp := viewport.New(80, 20)
	vp.SetContent("")

	return &UI{
		node:       node,
		messages:   []ChatMessage{},
		peers:      []string{},
		viewport:   vp,
		textarea:   ta,
		lastUpdate: time.Now(),
		showHelp:   false,
	}
}

// Init initializes the TUI
func (ui *UI) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		ui.listenForMessages(),
		ui.tickCmd(),
	)
}

// listenForMessages listens for messages from the node
func (ui *UI) listenForMessages() tea.Cmd {
	return func() tea.Msg {
		msg := <-ui.node.uiChannel
		return messageMsg(msg)
	}
}

// tickCmd sends periodic ticks to update peer list and status
func (ui *UI) tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages and updates the model
func (ui *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	ui.textarea, tiCmd = ui.textarea.Update(msg)
	ui.viewport, vpCmd = ui.viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			// Quit the application
			return ui, tea.Quit

		case tea.KeyCtrlH:
			// Toggle help
			ui.showHelp = !ui.showHelp
			ui.updateViewport()
			return ui, nil

		case tea.KeyEnter:
			// Send message
			input := strings.TrimSpace(ui.textarea.Value())
			if input != "" {
				// Handle commands
				if strings.HasPrefix(input, "/quit") || strings.HasPrefix(input, "/exit") {
					return ui, tea.Quit
				}

				// Send to CLI input channel
				ui.node.CLIInput <- input
				ui.textarea.Reset()
			}
			return ui, nil
		}

	case tea.WindowSizeMsg:
		ui.width = msg.Width
		ui.height = msg.Height

		if !ui.ready {
			ui.ready = true
		}

		// Update viewport size
		headerHeight := 3
		footerHeight := 5
		statusBarHeight := 1
		ui.viewport.Width = ui.width - 35 // Leave space for peer panel
		ui.viewport.Height = ui.height - headerHeight - footerHeight - statusBarHeight
		ui.textarea.SetWidth(ui.width - 4)

		ui.updateViewport()

	case messageMsg:
		// Add message to history
		chatMsg := ChatMessage{
			Sender:    msg.SenderID,
			Content:   string(msg.Content),
			Timestamp: time.Now(),
			IsSystem:  msg.SenderID == "System",
		}
		ui.messages = append(ui.messages, chatMsg)
		ui.updateViewport()

		// Auto-scroll to bottom
		ui.viewport.GotoBottom()

		// Continue listening for messages
		return ui, ui.listenForMessages()

	case tickMsg:
		// Update peer list periodically
		ui.updatePeerList()
		ui.lastUpdate = time.Time(msg)
		return ui, ui.tickCmd()
	}

	return ui, tea.Batch(tiCmd, vpCmd)
}

// updatePeerList updates the list of connected peers
func (ui *UI) updatePeerList() {
	ui.node.peersMutex.RLock()
	defer ui.node.peersMutex.RUnlock()

	ui.peers = make([]string, 0, len(ui.node.Peers))
	for peerID := range ui.node.Peers {
		ui.peers = append(ui.peers, peerID)
	}
}

// updateViewport updates the viewport content with all messages
func (ui *UI) updateViewport() {
	var content strings.Builder

	if ui.showHelp {
		content.WriteString(ui.renderHelp())
	} else {
		for _, msg := range ui.messages {
			content.WriteString(ui.renderMessage(msg))
			content.WriteString("\n")
		}
	}

	ui.viewport.SetContent(content.String())
}

// renderMessage renders a single message
func (ui *UI) renderMessage(msg ChatMessage) string {
	timestamp := timestampStyle.Render(msg.Timestamp.Format("15:04:05"))

	if msg.IsSystem {
		return fmt.Sprintf("%s %s",
			timestamp,
			systemMessageStyle.Render(msg.Content))
	}

	var senderStyle lipgloss.Style
	senderPrefix := ""

	if msg.Sender == ui.node.ID {
		senderStyle = userMessageStyle
		senderPrefix = "You"
	} else {
		senderStyle = peerMessageStyle
		senderPrefix = msg.Sender
	}

	sender := senderStyle.Render(fmt.Sprintf("[%s]", senderPrefix))
	return fmt.Sprintf("%s %s %s", timestamp, sender, msg.Content)
}

// renderHelp renders the help screen
func (ui *UI) renderHelp() string {
	help := `
╔══════════════════════════════════════════════════════════════════╗
║                        P2P CHAT - HELP                           ║
╚══════════════════════════════════════════════════════════════════╝

🔗 CONNECTION COMMANDS:
  /connect <addr>     Connect to a peer (e.g., /connect 127.0.0.1:8080)
  /peers              List all connected peers
  /discovered         List discovered peers via multicast

📁 FILE SHARING:
  /sendfile <peer> <path>  Send a file to a specific peer
                           Example: /sendfile 127.0.0.1:8080 ./file.txt

🎙️  VOICE MESSAGES:
  /voice <seconds>    Record and send voice message (1-60 seconds)
                      Example: /voice 10

🔒 ENCRYPTION:
  All messages are automatically encrypted with RSA 2048-bit encryption
  Public keys are exchanged automatically when peers connect

💬 MESSAGING:
  Just type and press Enter to send a message to all connected peers

⌨️  KEYBOARD SHORTCUTS:
  Ctrl+H              Toggle this help screen
  Ctrl+C / Esc        Quit application
  Enter               Send message

📊 STATUS:
  The right panel shows all connected peers in real-time
  System messages appear in green italics
  Your messages appear in purple
  Peer messages appear in blue

Press Ctrl+H to close this help screen
`
	return help
}

// View renders the TUI
func (ui *UI) View() string {
	if !ui.ready {
		return "\n  Initializing P2P Chat TUI...\n"
	}

	// Header
	header := headerStyle.Render("🚀 P2P Chat - Encrypted Peer-to-Peer Messaging")

	// Message panel (left side)
	messagePanel := messagePanelStyle.Width(ui.width - 35).Height(ui.viewport.Height + 2).Render(
		fmt.Sprintf("📨 Messages\n%s", ui.viewport.View()))

	// Peer panel (right side)
	peerPanel := ui.renderPeerPanel()

	// Combine panels side by side
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, messagePanel, peerPanel)

	// Status bar
	statusBar := ui.renderStatusBar()

	// Input area
	inputArea := inputStyle.Width(ui.width - 4).Render(
		fmt.Sprintf("💬 Input (Ctrl+H for help)\n%s", ui.textarea.View()))

	// Combine all sections
	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		mainContent,
		statusBar,
		inputArea,
	)
}

// renderPeerPanel renders the peer list panel
func (ui *UI) renderPeerPanel() string {
	var content strings.Builder

	content.WriteString("👥 Connected Peers\n")
	content.WriteString(strings.Repeat("─", 28) + "\n")

	if len(ui.peers) == 0 {
		content.WriteString(messagePanelStyle.Render("  No peers connected\n"))
		content.WriteString("\n")
		content.WriteString(messagePanelStyle.Render("  Use /connect <addr>\n"))
		content.WriteString(messagePanelStyle.Render("  to add peers\n"))
	} else {
		for i, peer := range ui.peers {
			peerStatus := peerConnectedStyle.Render("●")
			content.WriteString(fmt.Sprintf("  %s %s\n", peerStatus, peer))
			if i >= 15 { // Limit display to 15 peers
				remaining := len(ui.peers) - 15
				content.WriteString(fmt.Sprintf("  ... and %d more\n", remaining))
				break
			}
		}
	}

	// Fill remaining space
	panelHeight := ui.viewport.Height + 2
	currentLines := len(ui.peers) + 2
	if len(ui.peers) == 0 {
		currentLines = 6
	}
	for i := currentLines; i < panelHeight; i++ {
		content.WriteString("\n")
	}

	return peerPanelStyle.Width(30).Height(panelHeight).Render(content.String())
}

// renderStatusBar renders the bottom status bar
func (ui *UI) renderStatusBar() string {
	nodeInfo := fmt.Sprintf("Node: %s", ui.node.ID)
	peerCount := fmt.Sprintf("Peers: %d", len(ui.peers))
	encryption := "🔒 Encrypted"
	timestamp := ui.lastUpdate.Format("15:04:05")

	leftSection := nodeInfo
	rightSection := fmt.Sprintf("%s | %s | %s", peerCount, encryption, timestamp)

	// Calculate spacing
	totalWidth := ui.width - 4
	spacing := totalWidth - lipgloss.Width(leftSection) - lipgloss.Width(rightSection)
	if spacing < 0 {
		spacing = 0
	}

	statusText := leftSection + strings.Repeat(" ", spacing) + rightSection
	return statusBarStyle.Width(ui.width - 4).Render(statusText)
}
