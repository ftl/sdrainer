package rx

import (
	"regexp"
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

func TestTextWindow_FindNext(t *testing.T) {
	w := newTextWindow(10)
	aExp := regexp.MustCompile("a")

	_, found := w.FindNext(aExp, true)
	assert.False(t, found)
	assert.Equal(t, 0, w.searchPoint)

	w.Write([]byte("abc"))
	_, found = w.FindNext(aExp, true)
	assert.True(t, found)
	assert.Equal(t, 1, w.searchPoint)

	_, found = w.FindNext(aExp, true)
	assert.False(t, found)
	assert.Equal(t, 1, w.searchPoint)

	w.Write([]byte("1234567"))
	w.Shift()
	assert.Equal(t, 0, w.searchPoint)
	assert.Equal(t, "34567", w.String())

	w.Write([]byte("abc"))
	_, found = w.FindNext(aExp, true)
	assert.True(t, found)
	assert.Equal(t, 6, w.searchPoint)

	w.Shift()
	assert.Equal(t, 3, w.searchPoint)
	assert.Equal(t, "67abc", w.String())
}

func TestTextWindow_FindNext_IncludeTail(t *testing.T) {
	w := newTextWindow(10)
	abcExp := regexp.MustCompile("abc")

	w.Write([]byte("12345abc"))
	_, found := w.FindNext(abcExp, false)
	assert.False(t, found)

	_, found = w.FindNext(abcExp, true)
	assert.True(t, found)
}

func TestTextProcessor_CollectCallsign(t *testing.T) {
	p := NewTextProcessor(nil, WallClock, SpotReporterFunc(func(string) {}))
	p.Start()
	defer p.Stop()
	receivedText := "cq cq cq de dl1abc dl1abc dl1abc pse k"
	for _, c := range receivedText {
		p.Write([]byte(string(c)))
	}
	p.sync()

	t.Logf("collected callsigns %v", p.collectedCallsigns)
	assert.Equal(t, 3, p.collectedCallsigns["DL1ABC"].count)
}

func TestTextProcessor_WriteTimeout(t *testing.T) {
	p := NewTextProcessor(nil, WallClock, SpotReporterFunc(func(string) {}))
	p.Start()
	defer p.Stop()
	receivedText := "cq de dl1abc"
	for _, c := range receivedText {
		p.Write([]byte(string(c)))
	}
	p.sync()
	assert.Equal(t, 0, p.collectedCallsigns["DL1ABC"].count)

	p.op <- p.writeTimeout
	p.sync()
	assert.Equal(t, 1, p.collectedCallsigns["DL1ABC"].count)
}
