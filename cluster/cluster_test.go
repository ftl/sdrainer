package cluster

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_SpotMessage(t *testing.T) {
	tests := []struct {
		spotter   string
		callsign  string
		frequency int
		msg       string
		timestamp string
		expected  string
	}{
		{"local-#", "dl0abc", 14035000, "20 db 18 wpm  cq", "1651", "DX de local-#:   14035.0  dl0abc       20 db 18 wpm  cq               1651z\r\n"},
		{"local-#", "dl0abc", 7035000, "20 db 18 wpm  cq", "1652", "DX de local-#:    7035.0  dl0abc       20 db 18 wpm  cq               1652z\r\n"},
	}
	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			s := &Server[int]{mycall: test.spotter}
			timestamp, err := time.Parse("1504", test.timestamp)
			require.NoError(t, err)

			actual := s.formatSpotMessage(test.callsign, test.frequency, test.msg, timestamp)

			assert.Equal(t, test.expected, actual)
		})
	}
}

// TestConnectionAnswersCRLFOneTime covers the line end of a telnet client. CR and LF each ended the
// answer before, so a client that sends CR LF got two responses: the welcome and its spots, and
// then a further empty line from the answer to an empty string.
func TestConnectionAnswersCRLFOneTime(t *testing.T) {
	tt := []struct {
		name string
		line string
	}{
		{name: "CR LF, the telnet convention", line: "dl1xyz\r\n"},
		{name: "LF alone", line: "dl1xyz\n"},
		{name: "CR alone", line: "dl1xyz\r"},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			connection, client, _ := newTestConnection(t)
			defer connection.Close()

			_, err := client.Write([]byte(tc.line))
			require.NoError(t, err)

			assert.Equal(t, "welcome dl1xyz"+crlf, readOutput(t, client),
				"the answer gives the welcome exactly one time")
		})
	}
}

// TestConnectionUsesTheLoginPromptOfDXSpider holds the text of the prompt. A client looks for it
// literally: openhamclock compares against "login:", "Please enter your call" and
// "enter your callsign" and it respects the case, and clusterix takes the suffix "login:",
// "call:" or "callsign:" of the text in lower case.
func TestConnectionUsesTheLoginPromptOfDXSpider(t *testing.T) {
	connection, _, greeting := newTestConnection(t)
	defer connection.Close()

	prompt := strings.TrimPrefix(greeting, "SDRainer Version test"+crlf)

	assert.Equal(t, "login: ", prompt)
	assert.NotContains(t, strings.ToLower(prompt), "please enter",
		"openhamclock takes a prompt of that shape as a login that the node refused")
}

// newTestConnection gives a Connection over a real TCP pair and the end of the client. It reads the
// welcome away, so the next output is the prompt.
//
// A TCP pair and not net.Pipe: NewConnection writes the welcome before it returns, and the pipe of
// the standard library carries no buffer, so that write waits for a reader that does not exist yet.
func newTestConnection(t *testing.T) (*Connection, net.Conn, string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	type accepted struct {
		conn net.Conn
		err  error
	}
	incoming := make(chan accepted, 1)
	go func() {
		conn, err := listener.Accept()
		incoming <- accepted{conn, err}
	}()

	client, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })

	server := <-incoming
	require.NoError(t, server.err)

	connection := NewConnection(server.conn, "SDRainer Version test"+crlf, nil)

	// the welcome and the prompt go out one after the other and arrive together
	greeting := readOutput(t, client)
	require.Contains(t, greeting, "SDRainer Version test"+crlf)

	return connection, client, greeting
}

// readOutput reads what the connection wrote, until it writes nothing more for a moment.
func readOutput(t *testing.T, client net.Conn) string {
	t.Helper()

	var result strings.Builder
	buffer := make([]byte, 1024)
	for {
		require.NoError(t, client.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		n, err := client.Read(buffer)
		result.Write(buffer[:n])
		if err != nil {
			return result.String()
		}
	}
}
