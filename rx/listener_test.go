package rx

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIDPool(t *testing.T) {
	p := NewIDPool(10, "test")

	for i := 1; i <= 10; i++ {
		id, ok := p.Pop()
		assert.True(t, ok)
		assert.Equal(t, fmt.Sprintf("test%d", i), id)
	}

	_, nok := p.Pop()
	assert.False(t, nok)

	for i := 1; i <= 10; i++ {
		p.Push(fmt.Sprintf("test%d", i))
	}
	assert.Equal(t, 10, len(p))

	p.Push("one more")
	assert.Equal(t, 11, len(p))

	id, ok := p.Pop()
	assert.True(t, ok)
	assert.Equal(t, "one more", id)
}

func TestListenerPool(t *testing.T) {
	pool := NewListenerPool[float32, int](3, "test", func(id string) *Listener[float32, int] {
		return NewListener[float32, int](id, nil, WallClock, nil, 48000, 512)
	})

	listeners := make([]*Listener[float32, int], 0, 3)
	for i := 1; i <= 3; i++ {
		listener, ok := pool.BindNext()
		listeners = append(listeners, listener)
		assert.True(t, ok)
		assert.Equal(t, listener, pool.listeners[i-1])
		assert.Equal(t, fmt.Sprintf("test%d", i), listener.id)
	}

	_, nok := pool.BindNext()
	assert.False(t, nok)

	pool.Release(listeners[1])
	assert.Equal(t, 2, len(pool.listeners))
	assert.Equal(t, listeners[0], pool.listeners[0])
	assert.Equal(t, listeners[2], pool.listeners[1])

	pool.Release(listeners[0])
	assert.Equal(t, 1, len(pool.listeners))
	assert.Equal(t, listeners[2], pool.listeners[0])

	pool.Release(listeners[2])
	assert.Equal(t, 0, len(pool.listeners))

	newListener, ok := pool.BindNext()
	assert.True(t, ok)
	assert.Equal(t, listeners[2].id, newListener.id)
}
