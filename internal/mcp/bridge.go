package mcp

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// Bridge connects MCP stdio plugins to SSE transport.
type Bridge struct {
	mu      sync.Mutex
	plugins map[string]*Plugin
}

// Plugin is a running MCP stdio process plus SSE subscribers.
type Plugin struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	clients map[chan []byte]struct{}
}

func New() *Bridge {
	return &Bridge{plugins: make(map[string]*Plugin)}
}

// Start launches an MCP plugin process and fans stdout to subscribers.
func (b *Bridge) Start(name, command string, args []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p, ok := b.plugins[name]; ok && p.cmd != nil && p.cmd.Process != nil {
		return nil
	}
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}
	p := &Plugin{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewScanner(stdout),
		clients: make(map[chan []byte]struct{}),
	}
	// larger MCP messages
	buf := make([]byte, 0, 64*1024)
	p.stdout.Buffer(buf, 1024*1024)
	b.plugins[name] = p
	go b.readLoop(name, p)
	return nil
}

func (b *Bridge) readLoop(name string, p *Plugin) {
	for p.stdout.Scan() {
		line := append([]byte(nil), p.stdout.Bytes()...)
		p.mu.Lock()
		for ch := range p.clients {
			select {
			case ch <- line:
			default:
			}
		}
		p.mu.Unlock()
	}
	_ = p.cmd.Wait()
	b.mu.Lock()
	if cur, ok := b.plugins[name]; ok && cur == p {
		delete(b.plugins, name)
	}
	b.mu.Unlock()
	p.mu.Lock()
	for ch := range p.clients {
		close(ch)
	}
	p.clients = nil
	p.mu.Unlock()
}

// Send writes a JSON line to the plugin's stdin.
func (b *Bridge) Send(name string, msg []byte) error {
	b.mu.Lock()
	p := b.plugins[name]
	b.mu.Unlock()
	if p == nil {
		return fmt.Errorf("plugin %q not running", name)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdin == nil {
		return fmt.Errorf("plugin %q stdin closed", name)
	}
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg = append(append([]byte{}, msg...), '\n')
	}
	_, err := p.stdin.Write(msg)
	return err
}

// Subscribe returns a channel that receives plugin stdout lines.
// Caller must call Unsubscribe when done.
func (b *Bridge) Subscribe(name string) (chan []byte, error) {
	b.mu.Lock()
	p := b.plugins[name]
	b.mu.Unlock()
	if p == nil {
		return nil, fmt.Errorf("plugin %q not running", name)
	}
	ch := make(chan []byte, 32)
	p.mu.Lock()
	if p.clients == nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("plugin %q stopped", name)
	}
	p.clients[ch] = struct{}{}
	p.mu.Unlock()
	return ch, nil
}

// Unsubscribe removes a subscriber channel.
func (b *Bridge) Unsubscribe(name string, ch chan []byte) {
	b.mu.Lock()
	p := b.plugins[name]
	b.mu.Unlock()
	if p == nil {
		return
	}
	p.mu.Lock()
	if _, ok := p.clients[ch]; ok {
		delete(p.clients, ch)
		close(ch)
	}
	p.mu.Unlock()
}

// Running reports whether a plugin process is registered.
func (b *Bridge) Running(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.plugins[name]
	return ok
}
