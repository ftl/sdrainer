package cluster

import (
	"strconv"
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
		{"local-#", "dl0abc", 14035000, "20 db 18 wpm  cq", "1651", "DX de local-#:   14035.0  dl0abc       20 db 18 wpm  cq               1651z\n"},
		{"local-#", "dl0abc", 7035000, "20 db 18 wpm  cq", "1652", "DX de local-#:    7035.0  dl0abc       20 db 18 wpm  cq               1652z\n"},
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
