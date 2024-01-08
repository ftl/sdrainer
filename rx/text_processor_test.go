package rx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTextWindow_Write(t *testing.T) {
	const windowSize = 10
	tt := []struct {
		desc      string
		preset    string
		text      string
		expected  string
		expectedN int
		invalid   bool
	}{
		{
			desc:     "empty at start",
			expected: "",
		},
		{
			desc:      "append text",
			text:      "abc",
			expected:  "abc",
			expectedN: 3,
		},
		{
			desc:      "append text to existing",
			preset:    "123",
			text:      "abc",
			expected:  "123abc",
			expectedN: 3,
		},
		{
			desc:      "fill only current window",
			preset:    "1234567",
			text:      "abcdef",
			expected:  "1234567abc",
			expectedN: 3,
		},
		{
			desc:      "return error when window is already full",
			preset:    "1234567890",
			text:      "abcdef",
			expected:  "1234567890",
			expectedN: 0,
			invalid:   true,
		},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			w := newTextWindow(windowSize)
			w.window[w.currentWindow] = []byte(tc.preset)

			n, err := w.Write([]byte(tc.text))

			if tc.invalid {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expectedN, n)
			assert.Equal(t, tc.expected, w.String())
		})
	}
}

func TestTextWindow_Shift(t *testing.T) {
	w := newTextWindow(10)

	w.Shift()
	assert.Equal(t, 1, w.currentWindow)
	assert.Equal(t, "", w.String())

	_, err := w.Write([]byte("1234"))
	assert.NoError(t, err)
	w.Shift()
	assert.Equal(t, 0, w.currentWindow)
	assert.Equal(t, "1234", w.String())

	_, err = w.Write([]byte("123456"))
	assert.NoError(t, err)
	w.Shift()
	assert.Equal(t, 1, w.currentWindow)
	assert.Equal(t, "23456", w.String())

	_, err = w.Write([]byte("abcdefg"))
	assert.NoError(t, err)
	w.Shift()
	assert.Equal(t, 0, w.currentWindow)
	assert.Equal(t, "abcde", w.String())

	_, err = w.Write([]byte("fg"))
	assert.NoError(t, err)
	w.Shift()
	assert.Equal(t, 1, w.currentWindow)
	assert.Equal(t, "cdefg", w.String())

	w.Reset()
	assert.Equal(t, 0, w.currentWindow)
	assert.Equal(t, "", w.String())
}
