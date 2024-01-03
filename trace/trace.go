package trace

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

type Tracer interface {
	Context() string
	Start()
	Trace(context string, format string, args ...any)
	Stop()
}

type NoTracer struct{}

func (t *NoTracer) Context() string              { return "" }
func (t *NoTracer) Start()                       {}
func (t *NoTracer) Trace(string, string, ...any) {}
func (t *NoTracer) Stop()                        {}

type FileTracer struct {
	context  string
	filename string
	out      io.WriteCloser
}

func NewFileTracer(context string, filename string) *FileTracer {
	return &FileTracer{
		context:  context,
		filename: filename,
	}
}

func (t *FileTracer) Context() string {
	return t.context
}

func (t *FileTracer) Start() {
	if t.out != nil {
		return
	}

	var err error
	t.out, err = os.Create(t.filename)
	if err != nil {
		t.out = nil
		log.Printf("cannot start trace: %v", err)
	}
}

func (t *FileTracer) Trace(context string, format string, args ...any) {
	if t.out == nil {
		return
	}
	if context != t.context {
		return
	}

	fmt.Fprintf(t.out, format, args...)
}

func (t *FileTracer) Stop() {
	if t.out == nil {
		return
	}

	t.out.Close()
	t.out = nil
}

type UDPTracer struct {
	context string
	addr    *net.UDPAddr
	conn    *net.UDPConn
}

func NewUDPTracer(context string, destination string) *UDPTracer {
	addr, err := net.ResolveUDPAddr("udp", destination)
	if err != nil {
		log.Printf("cannot parse UDP destination: %v", err)
		return &UDPTracer{addr: nil}
	}
	return &UDPTracer{
		context: context,
		addr:    addr,
	}
}

func (t *UDPTracer) Context() string {
	return t.context
}

func (t *UDPTracer) Start() {
	if t.conn != nil {
		return
	}

	var err error
	t.conn, err = net.DialUDP("udp", nil, t.addr)
	if err != nil {
		t.conn = nil
		log.Printf("cannot start trace: %v", err)
	}
}

func (t *UDPTracer) Trace(context string, format string, args ...any) {
	if t.conn == nil {
		return
	}
	if context != t.context {
		return
	}

	fmt.Fprintf(t.conn, format, args...)
}

func (t *UDPTracer) Stop() {
	if t.conn == nil {
		return
	}

	t.conn.Close()
	t.conn = nil
}
