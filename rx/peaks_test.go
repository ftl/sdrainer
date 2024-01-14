package rx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ftl/sdrainer/dsp"
)

func TestPeaksTable_PutIntoEmptyTable(t *testing.T) {
	table := NewPeaksTable[float32, int](512, WallClock)
	peak := dsp.Peak[float32, int]{
		From: 234,
		To:   235,
	}

	table.Put(&peak)

	assert.Equal(t, &peak, table.bins[234].Peak)
	assert.Equal(t, &peak, table.bins[235].Peak)

	assert.Equal(t, peakNew, table.bins[234].state)
	assert.False(t, table.bins[234].since.IsZero())
}

func TestPeaksTable_Put(t *testing.T) {
	peak1 := peak[float32, int]{Peak: &dsp.Peak[float32, int]{From: 3, To: 4}, state: peakNew}
	peak2 := peak[float32, int]{Peak: &dsp.Peak[float32, int]{From: 5, To: 6}, state: peakNew}
	peak3 := peak[float32, int]{Peak: &dsp.Peak[float32, int]{From: 8, To: 8}, state: peakActive}
	peak4 := peak[float32, int]{Peak: &dsp.Peak[float32, int]{From: 10, To: 10}, state: peakInactive}
	table := PeaksTable[float32, int]{
		clock: WallClock,
		bins: []*peak[float32, int]{
			nil,
			nil,
			nil,
			&peak1,
			&peak1,
			&peak2,
			&peak2,
			nil,
			&peak3,
			nil,
			&peak4,
			nil,
		},
	}

	newPeak1 := dsp.Peak[float32, int]{From: 1, To: 2}
	newPeak2 := dsp.Peak[float32, int]{From: 4, To: 5}
	newPeak3 := dsp.Peak[float32, int]{From: 7, To: 8}
	newPeak4 := dsp.Peak[float32, int]{From: 10, To: 11}
	table.Put(&newPeak1)
	table.Put(&newPeak2)
	table.Put(&newPeak3)
	table.Put(&newPeak4)

	assert.Nil(t, table.bins[0])
	assert.Equal(t, &newPeak1, table.bins[1].Peak)
	assert.Equal(t, &newPeak1, table.bins[2].Peak)
	assert.Nil(t, table.bins[3])
	assert.Equal(t, &newPeak2, table.bins[4].Peak)
	assert.Equal(t, &newPeak2, table.bins[5].Peak)
	assert.Nil(t, table.bins[6])
	assert.Nil(t, table.bins[7])
	assert.Equal(t, &peak3, table.bins[8])
	assert.Nil(t, table.bins[9])
	assert.Equal(t, &peak4, table.bins[10])
	assert.Nil(t, table.bins[11])
}

func TestPeaksTable_Cleanup_NewPeak(t *testing.T) {
	clock := new(manualClock)
	clock.Set(time.Now())
	table := NewPeaksTable[float32, int](512, clock)
	peak := dsp.Peak[float32, int]{
		From: 234,
		To:   235,
	}

	table.Put(&peak)
	table.Cleanup()

	assert.Equal(t, &peak, table.bins[234].Peak)
	assert.Equal(t, &peak, table.bins[235].Peak)

	clock.Add(table.peakTimeout + 1*time.Second)
	table.Cleanup()

	assert.Nil(t, table.bins[234])
	assert.Nil(t, table.bins[235])
}

func TestPeaksTable_Cleanup_InactivePeak(t *testing.T) {
	clock := new(manualClock)
	clock.Set(time.Now())
	table := NewPeaksTable[float32, int](512, clock)
	peak := dsp.Peak[float32, int]{
		From: 234,
		To:   235,
	}

	table.Put(&peak)
	table.Cleanup()

	assert.Equal(t, &peak, table.bins[234].Peak)
	assert.Equal(t, &peak, table.bins[235].Peak)

	table.Activate(&peak)

	clock.Add(table.peakTimeout + 1*time.Second)
	table.Cleanup()

	assert.Equal(t, &peak, table.bins[234].Peak)
	assert.Equal(t, &peak, table.bins[235].Peak)

	table.Deactivate(&peak)
	table.Cleanup()

	assert.Nil(t, table.bins[234])
	assert.Nil(t, table.bins[235])
}

func TestPeaksTable_FindNext(t *testing.T) {
	table := NewPeaksTable[float32, int](512, WallClock)
	peak := dsp.Peak[float32, int]{
		From: 234,
		To:   235,
	}

	table.Put(&peak)

	next := table.FindNext()
	assert.Equal(t, &peak, next)

	table.Activate(next)
	assert.Nil(t, table.FindNext())

	table.Deactivate(next)
	assert.Nil(t, table.FindNext())
}
