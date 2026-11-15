// Copyright 2026 LiveKit, Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package sip

import (
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	msdk "github.com/livekit/media-sdk"
	"github.com/livekit/protocol/logger"
	"github.com/livekit/sip/pkg/config"
	"github.com/stretchr/testify/require"
)

type mixerProbe struct {
	nonzero int
	samples int
}

func (*mixerProbe) SampleRate() int { return RoomSampleRate }
func (*mixerProbe) String() string  { return "mixer-probe" }
func (*mixerProbe) Close() error    { return nil }
func (p *mixerProbe) WriteSample(sample msdk.PCM16Sample) error {
	for _, v := range sample {
		p.samples++
		if v != 0 {
			p.nonzero++
		}
	}
	return nil
}

// No wall-clock timing or telephone connection: fake time gives the upstream
// mixer alternating 20/180 ms deliveries with the same 100 ms average supply.
// Continuous nonzero input distinguishes starvation from normal Opus silence.
func TestConfiguredRoomMixerBurstyDelivery(t *testing.T) {
	for _, size := range []int{0, 15} {
		t.Run(fmt.Sprintf("frames%d", size), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				out, stats := &mixerProbe{}, &RoomStats{}
				r := configuredGetRoomFunc(&config.Config{MixerInputBufferFrames: size})(logger.NewTestLogger(t), stats).(*Room)
				defer r.Close()
				r.WriteOutboundAudioTo(out)
				input := r.NewTrack()
				frame := make(msdk.PCM16Sample, RoomSampleRate/50)
				for i := range frame {
					frame[i] = 1000
				}
				for burst := range 20 {
					for range 5 {
						require.NoError(t, input.WriteSample(frame))
					}
					if burst%2 == 0 {
						time.Sleep(180 * time.Millisecond)
					} else {
						time.Sleep(20 * time.Millisecond)
					}
					synctest.Wait()
				}
				// Drain the bounded playout tail before measuring retained speech.
				time.Sleep(400 * time.Millisecond)
				synctest.Wait()
				if size == 0 {
					require.Positive(t, stats.Mixer.InputSamplesDropped.Load())
					require.Greater(t, stats.Mixer.Restarts.Load(), uint64(1))
				} else {
					require.Zero(t, stats.Mixer.InputSamplesDropped.Load())
					require.Equal(t, uint64(1), stats.Mixer.Restarts.Load(), "only the final drained tail may restart")
					require.Equal(t, 100*len(frame), out.nonzero, "all continuous speech must survive")
				}
			})
		})
	}
}
