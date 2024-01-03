package trace

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

type Tracer interface {
	Start()
	Trace(format string, args ...any)
	Stop()
}

type NoTracer struct{}

func (t *NoTracer) Start()               {}
func (t *NoTracer) Trace(string, ...any) {}
func (t *NoTracer) Stop()                {}

type FileTracer struct {
	filename string
	out      io.WriteCloser
}

func NewFileTracer(filename string) *FileTracer {
	return &FileTracer{
		filename: filename,
	}
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

func (t *FileTracer) Trace(format string, args ...any) {
	if t.out == nil {
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
	addr *net.UDPAddr
	conn *net.UDPConn
}

func NewUDPTracer(destination string) *UDPTracer {
	addr, err := net.ResolveUDPAddr("udp", destination)
	if err != nil {
		log.Printf("cannot parse UDP destination: %v", err)
		return &UDPTracer{addr: nil}
	}
	return &UDPTracer{addr: addr}
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

func (t *UDPTracer) Trace(format string, args ...any) {
	if t.conn == nil {
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
