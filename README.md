# P2P Chat - Encrypted Peer-to-Peer Messaging

A feature-rich, decentralized chat application built in Go with end-to-end encryption, file sharing, voice messaging, and a beautiful terminal user interface.

## Features

- **🔒 End-to-End Encryption**: All messages encrypted with RSA 2048-bit encryption
- **🌐 Peer-to-Peer Architecture**: Direct connections between peers, no central server
- **🔍 Auto-Discovery**: Automatic peer discovery via UDP multicast
- **📁 File Sharing**: Send files to specific peers with chunked transfers and MD5 verification
- **🎙️ Voice Messaging**: Record and send voice messages (requires ffmpeg)
- **💬 Beautiful TUI**: Modern terminal user interface with split-pane layout and real-time updates
- **📊 Gossip Protocol**: Peer list propagation for network resilience

## Screenshots

### Terminal User Interface (TUI)
```
┌────────────────────────────────────────────────────────────────────┐
│ 🚀 P2P Chat - Encrypted Peer-to-Peer Messaging                    │
├──────────────────────────────────┬─────────────────────────────────┤
│ 📨 Messages                      │ 👥 Connected Peers              │
│                                  │ ────────────────────────────    │
│ 15:04:05 [System] 🔗 Peer        │  ● 127.0.0.1:51234              │
│          connected: 127.0.0.1... │  ● 192.168.1.100:8080           │
│ 15:04:12 [You] Hello everyone!   │                                 │
│ 15:04:15 [127.0.0.1:51234]       │  Use /connect <addr>            │
│          Hi there!                │  to add peers                   │
│                                  │                                 │
├──────────────────────────────────┴─────────────────────────────────┤
│ Node: 127.0.0.1:8080      Peers: 2 | 🔒 Encrypted | 15:04:20      │
├────────────────────────────────────────────────────────────────────┤
│ 💬 Input (Ctrl+H for help)                                        │
│ ┃ _                                                                │
└────────────────────────────────────────────────────────────────────┘
```

## Prerequisites

- **Go 1.21 or higher**
- **ffmpeg** (for voice messaging feature)
- **ALSA libraries** (for audio on Linux): `libasound2-dev`

## Installation

### From Source

```bash
# Clone the repository
git clone <repository-url>
cd p2pchat-go

# Install dependencies
go mod download

# Build the application
go build -o bin/p2pchat

# Run
./bin/p2pchat --tui
```

### Quick Start (No Build)

```bash
go run . --tui
```

## Usage

### Starting the Application

**With TUI (Recommended):**
```bash
./p2pchat --tui
```

**With CLI (Legacy):**
```bash
./p2pchat --gui=false
```

**With Custom Port:**
```bash
./p2pchat --tui --listen :8080
```

**Connect to Initial Peers:**
```bash
./p2pchat --tui --peer 127.0.0.1:8080 --peer 192.168.1.100:8080
```

**Disable Auto-Discovery:**
```bash
./p2pchat --tui --no-discovery
```

### TUI Controls

| Key Binding | Action |
|-------------|--------|
| `Ctrl+H` | Toggle help screen |
| `Ctrl+C` / `Esc` | Quit application |
| `Enter` | Send message |
| `↑` / `↓` | Scroll message history (in viewport) |

### Commands

| Command | Description | Example |
|---------|-------------|---------|
| `/connect <addr>` | Connect to a peer | `/connect 127.0.0.1:8080` |
| `/peers` | List all connected peers | `/peers` |
| `/discovered` | List discovered peers | `/discovered` |
| `/sendfile <peer> <path>` | Send a file to a peer | `/sendfile 127.0.0.1:8080 ./document.pdf` |
| `/voice <seconds>` | Record and send voice message (1-60s) | `/voice 10` |
| `/help` | Show help | `/help` |
| `/quit` | Exit application | `/quit` |

## Architecture

### Core Components

1. **Node** (`node.go`, `node_impl.go`): Core P2P networking logic
   - TCP listener for incoming connections
   - Peer management with concurrent-safe maps
   - Event-driven architecture with channels

2. **EnhancedNode** (`integration.go`): Feature wrapper
   - Integrates encryption, file sharing, and voice messaging
   - Message routing based on type
   - Peer ID mapping for ephemeral connections

3. **CryptoManager** (`crypto.go`): End-to-end encryption
   - RSA 2048-bit key generation
   - Automatic key exchange on peer connection
   - Message encryption/decryption with OAEP

4. **FileTransferManager** (`file_sharing.go`): Chunked file transfers
   - 8KB chunks with sequential numbering
   - MD5 checksum verification
   - Automatic assembly on completion

5. **VoiceMessageManager** (`voice_messaging.go`): Audio messaging
   - ffmpeg integration for recording
   - MP3 encoding
   - Audio playback with beep library

6. **DiscoveryService** (`discovery.go`): Peer discovery
   - UDP multicast on 239.255.255.250:9999
   - Periodic announcements every 5 seconds
   - Gossip protocol for peer list propagation

7. **TUI** (`tui.go`): Terminal User Interface
   - Bubbletea framework for reactive UI
   - Split-pane layout (messages | peers)
   - Real-time updates and message history
   - Viewport for scrolling, textarea for input

### Message Flow

```
User Input → CLIInput channel → EnhancedNode
                                     ↓
                         Encrypt with peer's public key
                                     ↓
                         Broadcast to all peers
                                     ↓
Peer receives → IncomingMsg channel → Decrypt → Route by type
                                                      ↓
                                            text / file / voice
                                                      ↓
                                              uiChannel → TUI
```

### Security

- **RSA 2048-bit encryption** for all messages
- **Automatic key exchange** on peer connection (unencrypted, public keys only)
- **OAEP padding** with SHA-256
- **Separate encryption** for each peer (no key reuse)
- **Ephemeral connections**: Connection ports differ from listen ports

## Configuration

All configuration is currently done via command-line flags:

```bash
Flags:
  -listen string
        address to listen on (default ":0" for auto-assign)
  -peer value
        peer address to connect to (can be specified multiple times)
  -no-discovery
        disable auto-discovery via multicast
  -tui
        use beautiful TUI interface (recommended)
  -gui
        use cross-platform GUI (default, but not implemented)
```

## Troubleshooting

### Build Errors

**"Package alsa was not found"**
```bash
# On Ubuntu/Debian
sudo apt-get install libasound2-dev

# On Fedora/RHEL
sudo dnf install alsa-lib-devel

# On Arch
sudo pacman -S alsa-lib
```

**"Failed to connect to peer"**
- Check firewall settings
- Verify the peer address and port are correct
- Ensure the peer is listening

### Runtime Issues

**"No peers discovered"**
- Multicast may be blocked on your network
- Try manual connection with `/connect <addr>`
- Check if `--no-discovery` flag was used by mistake

**"Failed to encrypt message"**
- Wait a few seconds for key exchange to complete
- Reconnect to the peer with `/connect`

**Voice messaging not working**
- Ensure ffmpeg is installed: `ffmpeg -version`
- Check audio device permissions

## Development

### Project Structure

```
p2pchat-go/
├── main.go              # Entry point and CLI flags
├── types.go             # Core data structures
├── node.go              # Node initialization
├── node_impl.go         # Node implementation
├── integration.go       # EnhancedNode with features
├── message.go           # Message handling
├── crypto.go            # Encryption/decryption
├── file_sharing.go      # File transfer logic
├── voice_messaging.go   # Voice recording/playback
├── discovery.go         # Peer discovery via UDP
├── tui.go               # Terminal user interface
├── gui.go               # GUI stub (not implemented)
├── go.mod               # Go module dependencies
└── README.md            # This file
```

### Building

```bash
# Development build
go build -o bin/p2pchat

# Production build with optimizations
go build -ldflags="-s -w" -o bin/p2pchat

# Cross-compilation for Windows
GOOS=windows GOARCH=amd64 go build -o bin/p2pchat.exe

# Cross-compilation for macOS
GOOS=darwin GOARCH=amd64 go build -o bin/p2pchat-mac
```

### Testing

```bash
# Run two instances locally
./p2pchat --tui --listen :8080
./p2pchat --tui --listen :8081 --peer 127.0.0.1:8080

# Test with discovery disabled
./p2pchat --tui --no-discovery --listen :8080
./p2pchat --tui --no-discovery --listen :8081 --peer 127.0.0.1:8080
```

## Known Limitations

1. **No message persistence**: Messages are not saved to disk
2. **No user authentication**: Anyone can connect if they know your address
3. **No group chat rooms**: All messages are broadcast to all peers
4. **Voice requires ALSA**: Audio features need system audio libraries
5. **GUI not implemented**: Only TUI and CLI modes are functional

## Roadmap

- [ ] Message history persistence
- [ ] User profiles and authentication
- [ ] Group/room support
- [ ] WebRTC for NAT traversal
- [ ] Mobile client
- [ ] Desktop GUI implementation
- [ ] End-to-end encrypted file storage

## Contributing

Contributions are welcome! Please feel free to submit pull requests or open issues for bugs and feature requests.

## License

[Specify your license here]

## Credits

Built with:
- [Bubbletea](https://github.com/charmbracelet/bubbletea) - Terminal UI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [Beep](https://github.com/faiface/beep) - Audio playback

## Support

For issues, questions, or feature requests, please open an issue on GitHub.
