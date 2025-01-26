package scope

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartStopScope(t *testing.T) {
	scope := NewScope("localhost:")

	err := scope.Start()
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	assert.True(t, scope.Active())

	scope.Stop()
	time.Sleep(10 * time.Millisecond)
	assert.False(t, scope.Active())
}
