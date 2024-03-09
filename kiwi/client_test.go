package kiwi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeKiwiMessage(t *testing.T) {
	tt := []struct {
		desc            string
		bytes           []byte
		expectedTag     kiwiTag
		expectedPayload []byte
		invalid         bool
	}{
		{
			desc:    "too short",
			bytes:   []byte{1, 2},
			invalid: true,
		},
		{
			desc:            "only tag",
			bytes:           []byte("MSG"),
			expectedTag:     msgTag,
			expectedPayload: []byte{},
		},
		{
			desc:            "tag with payload",
			bytes:           []byte("MSG1"),
			expectedTag:     msgTag,
			expectedPayload: []byte{'1'},
		},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			tag, payload, err := decodeKiwiMessage(tc.bytes)
			if tc.invalid {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedTag, tag)
				assert.Equal(t, tc.expectedPayload, payload)
			}
		})
	}
}

func TestDecodeConfigurationMessage(t *testing.T) {
	tt := []struct {
		desc                  string
		payload               string
		expectedConfiguration kiwiConfiguration
		invalid               bool
	}{
		{
			desc:    "single key/value pair",
			payload: "freq_offset=0.000",
			expectedConfiguration: kiwiConfiguration{
				"freq_offset": "0.000",
			},
		},
		{
			desc:    "multiple key/value pairs",
			payload: "center_freq=15000000 bandwidth=30000000 adc_clk_nom=66666600",
			expectedConfiguration: kiwiConfiguration{
				"center_freq": "15000000",
				"bandwidth":   "30000000",
				"adc_clk_nom": "66666600",
			},
		},
		{
			desc:    "url encoded key/value pair",
			payload: "load_cfg=%7B%0A%20%20%22test%22%3A%20%7B%0A%20%20%20%20%22setting1%22%3A%20%22value%22%2C%0A%20%20%20%20%22setting2%22%3A%20true%0A%20%20%7D%0A%7D%0A",
			expectedConfiguration: kiwiConfiguration{
				"load_cfg": `{
  "test": {
    "setting1": "value",
    "setting2": true
  }
}
`,
			},
		},
		{
			desc:    "too_busy",
			payload: tooBusyMessage + "=1",
			invalid: true,
		},
		{
			desc:    "bad_password",
			payload: badPasswordMessage + "=1",
			invalid: true,
		},
		{
			desc:    "down",
			payload: downMessage + "=1",
			invalid: true,
		},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			client := &Client{
				configuration: make(kiwiConfiguration),
			}
			err := client.decodeConfigurationMessage([]byte(tc.payload))
			if tc.invalid {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedConfiguration, client.configuration)
			}
		})
	}
}
