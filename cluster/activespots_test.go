package cluster

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ftl/hamradio/callsign"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSpotTime is the timestamp of each spot of these tests, so the lines are the same in each run.
var testSpotTime = time.Date(2026, 8, 22, 16, 51, 0, 0, time.UTC)

func newTestServer() *Server[int] {
	return &Server[int]{
		mycall:        "local-#",
		lastSpots:     make(map[spotHash]time.Time),
		activeSpots:   make(map[string]activeSpot),
		silencePeriod: defaultSpotSilencePeriod,
		// the buffer takes the messages of the tests, so that Spot does not wait for a reader
		msg: make(chan []byte, 16),
	}
}

// spotKeyOf gives the callsign as the pipeline gives it to RemoveSpot: it comes from a
// callsign.Callsign, and that type does not always use the case of the text that made it.
func spotKeyOf(t *testing.T, call string) string {
	t.Helper()

	result, err := callsign.Parse(call)
	require.NoError(t, err)

	return result.String()
}

// TestActiveSpotsHoldEachStation is the base case: each station that gave a spot stands in the list
// for a new connection.
func TestActiveSpotsHoldEachStation(t *testing.T) {
	server := newTestServer()

	server.Spot("dl0abc", 14035000, "CW 20 WPM 18 dB", testSpotTime)
	server.Spot("dl0xyz", 14021000, "CW 25 WPM 22 dB", testSpotTime)

	messages := server.activeSpotMessages()

	require.Len(t, messages, 2)
	assert.Contains(t, messages[0], "dl0xyz", "the lowest frequency comes first")
	assert.Contains(t, messages[1], "dl0abc")
}

// TestActiveSpotsForgetAStationThatIsGone is the reason for RemoveSpot: a station that went away
// must not go to a new connection.
func TestActiveSpotsForgetAStationThatIsGone(t *testing.T) {
	server := newTestServer()
	server.Spot("dl0abc", 14035000, "CW 20 WPM 18 dB", testSpotTime)
	server.Spot("dl0xyz", 14021000, "CW 25 WPM 22 dB", testSpotTime)

	server.RemoveSpot(spotKeyOf(t, "dl0abc"))

	messages := server.activeSpotMessages()

	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], "dl0xyz")
}

// TestActiveSpotsIgnoreAStationWithoutACallsign covers each channel that never gave a spot. The
// pipeline destroys many more channels than it spots.
func TestActiveSpotsIgnoreAStationWithoutACallsign(t *testing.T) {
	server := newTestServer()
	server.Spot("dl0abc", 14035000, "CW 20 WPM 18 dB", testSpotTime)

	server.RemoveSpot("")

	assert.Len(t, server.activeSpotMessages(), 1)
}

// TestActiveSpotsHoldTheNewestSpotOfAStation covers the station whose callsign the pipeline
// corrects: the list holds one entry, with the newer text.
func TestActiveSpotsHoldTheNewestSpotOfAStation(t *testing.T) {
	server := newTestServer()

	server.Spot("dl0abc", 14035000, "CW 20 WPM 18 dB", testSpotTime)
	server.Spot("dl0abc", 14035000, "CW 24 WPM 25 dB", testSpotTime.Add(time.Minute))

	messages := server.activeSpotMessages()

	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], "24 WPM")
}

// TestActiveSpotsKeepAStationThatTheSilencePeriodHoldsBack is the case that a list of the recent
// spots would lose. The pipeline gives the callsign of a running station one time, so the spot of a
// station that runs for a long time is old, and that station is exactly the one that a new
// connection needs.
func TestActiveSpotsKeepAStationThatTheSilencePeriodHoldsBack(t *testing.T) {
	server := newTestServer()
	server.SetSilencePeriod(time.Minute)

	server.Spot("dl0abc", 14035000, "CW 20 WPM 18 dB", testSpotTime)
	<-server.msg // the announcement of that spot

	// the same station one hour later: the silence period is over, so this one is announced again
	server.Spot("dl0abc", 14035000, "CW 20 WPM 18 dB", testSpotTime.Add(time.Hour))

	assert.Len(t, server.activeSpotMessages(), 1)
}

// TestConnectionSendsTheActiveSpotsAfterTheLogin checks the moment: a DX cluster client answers the
// prompt and it reads the spots after that. The test uses a real TCP connection, so it also covers
// the way through the server.
func TestConnectionSendsTheActiveSpotsAfterTheLogin(t *testing.T) {
	server, err := NewServer[int]("localhost:0", "local-#", "test")
	require.NoError(t, err)
	defer server.Stop()

	server.Spot("dl0abc", 14035000, "CW 20 WPM 18 dB", testSpotTime)
	server.Spot("dl0xyz", 14021000, "CW 25 WPM 22 dB", testSpotTime)

	client, err := net.DialTimeout("tcp", server.listener.Addr().String(), time.Second)
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, client.SetDeadline(time.Now().Add(5*time.Second)))

	reader := bufio.NewReader(client)
	welcome, err := reader.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, welcome, "SDRainer Version test")

	prompt, err := readUntil(reader, "Enter your callsign: ")
	require.NoError(t, err)
	assert.Contains(t, prompt, "Enter your callsign: ")

	_, err = fmt.Fprint(client, "dl1xyz\n")
	require.NoError(t, err)

	// the welcome of the user and one line for each station that has a channel
	lines := make([]string, 0, 3)
	for range 3 {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		lines = append(lines, line)
	}

	assert.Contains(t, lines[0], "welcome dl1xyz")
	assert.Contains(t, lines[1], "dl0xyz", "the lowest frequency comes first")
	assert.Contains(t, lines[2], "dl0abc")
}

// readUntil reads from the given reader until the text is complete. The prompt has no line break,
// so ReadString does not find it.
func readUntil(reader *bufio.Reader, text string) (string, error) {
	var result strings.Builder
	for !strings.Contains(result.String(), text) {
		value, err := reader.ReadByte()
		if err != nil {
			return result.String(), err
		}
		result.WriteByte(value)
	}
	return result.String(), nil
}
