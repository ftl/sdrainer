package telnet

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_SpotMessage(t *testing.T) {
	expected := "DX de local-#:   14035.0  dl0abc       20 db 18 wpm  cq               1651z\n"
	s := &Server{mycall: "local-#"}
	timestamp, err := time.Parse("1504", "1651")
	require.NoError(t, err)

	actual := s.formatSpotMessage("dl0abc", 14035000, "20 db 18 wpm  cq", timestamp)

	assert.Equal(t, expected, actual)
}
