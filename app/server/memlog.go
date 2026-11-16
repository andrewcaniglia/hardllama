package server

import (
	"bytes"
	"io"
	"os"
	"sync"
)

// By default the server's output is kept in memory rather than written to
// disk, because it names the models being used and when. Setting
// HARDLLAMA_SERVER_LOG=1 writes it to serverLogPath instead, for debugging.
var serverLogOnDisk = os.Getenv("HARDLLAMA_SERVER_LOG") == "1"

var serverLogMemory = &memoryLog{}

const (
	// The start of the log holds the hardware details GetInferenceInfo looks
	// for, so it is kept along with the most recent output.
	memoryLogHeadSize = 256 << 10
	memoryLogTailSize = 768 << 10
)

// memoryLog keeps the start and the end of what is written to it.
type memoryLog struct {
	mu   sync.Mutex
	head []byte
	tail []byte
}

func (m *memoryLog) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	n := len(p)
	if room := memoryLogHeadSize - len(m.head); room > 0 {
		k := min(room, len(p))
		m.head = append(m.head, p[:k]...)
		p = p[k:]
	}
	if len(p) > 0 {
		m.tail = append(m.tail, p...)
		// Trimming only once the tail is twice its size keeps copying rare.
		if len(m.tail) > 2*memoryLogTailSize {
			m.tail = append([]byte(nil), m.tail[len(m.tail)-memoryLogTailSize:]...)
		}
	}
	return n, nil
}

func (m *memoryLog) Close() error { return nil }

func (m *memoryLog) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.head, m.tail = nil, nil
}

// Bytes returns a copy of the start and the most recent part of the log.
func (m *memoryLog) Bytes() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	tail := m.tail
	if len(tail) > memoryLogTailSize {
		tail = tail[len(tail)-memoryLogTailSize:]
	}
	out := make([]byte, 0, len(m.head)+1+len(tail))
	out = append(out, m.head...)
	if len(tail) > 0 {
		out = append(out, '\n')
		out = append(out, tail...)
	}
	return out
}

// openServerLog returns where the server's output for a new run goes.
func openServerLog() (io.WriteCloser, error) {
	if serverLogOnDisk {
		return openRotatingLog()
	}
	serverLogMemory.reset()
	return serverLogMemory, nil
}

// readServerLog returns the server's output for the current run.
func readServerLog() (io.ReadCloser, error) {
	if serverLogOnDisk {
		return os.Open(serverLogPath)
	}
	return io.NopCloser(bytes.NewReader(serverLogMemory.Bytes())), nil
}
