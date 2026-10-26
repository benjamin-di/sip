// Copyright 2026 LiveKit, Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package sip

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	msdk "github.com/livekit/media-sdk"
	"github.com/livekit/media-sdk/dtmf"
	"github.com/livekit/media-sdk/g711"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/livekit/sip/pkg/config"
	"github.com/stretchr/testify/require"
)

// Cover the actual room data-packet callback used by publish_dtmf, rather than
// only injecting a digit train directly into the media pipeline's connector.
func TestRoomKeypadPacketsReachTelephoneAudio(t *testing.T) {
	h := newPipelineHarness(t, RoomSampleRate)
	c := audioCodecByName(t, g711.ULawSDPNameOnly)
	h.configureType(c, 8000, testAudioPT(c), testDTMFPT, false, "audio")
	r := NewRoom(logger.NewTestLogger(t), &RoomStats{})
	t.Cleanup(func() { _ = r.Close() })
	audio, digits := h.pipeline.GetConnectors()
	r.WriteOutboundAudioTo(audio)
	r.WriteOutboundDTMFTo(digits)
	track := r.NewTrack()
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		frame := make(msdk.PCM16Sample, RoomSampleRate/50)
		for i := range frame {
			frame[i] = int16(1000 * math.Sin(2*math.Pi*440*float64(i)/RoomSampleRate))
		}
		for {
			select {
			case <-ticker.C:
				_ = track.WriteSample(frame)
			case <-stop:
				return
			}
		}
	}()
	t.Cleanup(func() { close(stop); <-stopped })
	h.drainRemote()
	callback := r.newRoomCallback(&config.Config{}, RoomConfig{})
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for _, digit := range "12#" {
			callback.OnDataPacket(&livekit.SipDTMF{Digit: string(digit)}, lksdk.DataReceiveParams{})
		}
	}()
	var pcm msdk.PCM16Sample
	var completed time.Time
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-sent:
			if completed.IsZero() {
				completed = time.Now()
			}
		default:
		}
		if !completed.IsZero() && time.Since(completed) > 300*time.Millisecond {
			break
		}
		packet, ok := h.readRemotePacket(20 * time.Millisecond)
		if !ok {
			continue
		}
		require.NotEqual(t, testDTMFPT, packet.PayloadType, "audio-only mode must not add duplicate events")
		if packet.PayloadType == h.audio.Type {
			pcm = append(pcm, g711.ULawSample(packet.Payload).Decode()...)
		}
	}
	require.False(t, completed.IsZero())
	require.EqualValues(t, 3, r.stats.dataPackets.Load())
	require.Equal(t, "12#", decodeTestDTMF(pcm), "room keypad packets must survive G.711 under concurrent voice")
}

func TestOutboundDTMFSequenceAndPCM(t *testing.T) {
	for _, codec := range []string{g711.ULawSDPNameOnly, g711.ALawSDPNameOnly} {
		for _, rate := range []int{8000, 24000, 48000} {
			for _, mode := range []string{"rtp", "audio", "both"} {
				audible := mode != "rtp"
				t.Run(fmt.Sprintf("%s/room%d/%s", codec, rate, mode), func(t *testing.T) {
					t.Parallel()
					h := newPipelineHarness(t, rate)
					c := audioCodecByName(t, codec)
					h.configureType(c, 8000, testAudioPT(c), testDTMFPT, false, mode)
					h.drainRemote()
					sent := make(chan error, 1)
					go func() { sent <- h.dtmfOut.WriteSample("12#") }()
					var ended []byte
					endings := make(map[byte]int)
					var pcm msdk.PCM16Sample
					var doneAt time.Time
					deadline := time.Now().Add(4 * time.Second)
					for time.Now().Before(deadline) {
						select {
						case err := <-sent:
							require.NoError(t, err)
							doneAt = time.Now()
						default:
						}
						if !doneAt.IsZero() && time.Since(doneAt) > 150*time.Millisecond {
							break
						}
						packet, ok := h.readRemotePacket(20 * time.Millisecond)
						if !ok {
							continue
						}
						switch packet.PayloadType {
						case testDTMFPT:
							event, err := dtmf.Decode(packet.Payload)
							require.NoError(t, err)
							if event.End {
								if endings[event.Digit] == 0 {
									ended = append(ended, event.Digit)
								}
								endings[event.Digit]++
								require.InDelta(t, 2000, int(event.Dur), 80)
							}
						case h.audio.Type:
							if codec == g711.ULawSDPNameOnly {
								pcm = append(pcm, g711.ULawSample(packet.Payload).Decode()...)
							} else {
								pcm = append(pcm, g711.ALawSample(packet.Payload).Decode()...)
							}
						}
					}
					require.False(t, doneAt.IsZero(), "digit train must finish")
					if mode == "audio" {
						require.Empty(t, ended, "in-band mode must not send duplicate telephone events")
					} else {
						require.Equal(t, "12#", string(ended), "receiver must see the complete ordered sequence")
					}
					for _, digit := range []byte("12#") {
						if mode == "audio" {
							continue
						}
						require.Equal(t, 3, endings[digit], "RFC 4733 repeats each end three times")
					}
					detected := decodeTestDTMF(pcm)
					if audible {
						require.Equal(t, "12#", detected, "decoded telephone audio must contain the three tone pairs")
					} else {
						require.Empty(t, detected, "event-only signalling must not synthesize tone audio")
					}
				})
			}
		}
	}
}

func decodeTestDTMF(pcm msdk.PCM16Sample) string {
	low := []int{697, 770, 852, 941}
	high := []int{1209, 1336, 1477, 1633}
	keypad := "123A456B789C*0#D"
	var result []byte
	var previous byte
	run := 0
	for offset := 0; offset+160 <= len(pcm); offset += 160 {
		frame := pcm[offset : offset+160]
		strongest := func(frequencies []int) (int, float64) {
			powers := make([]float64, len(frequencies))
			for i, freq := range frequencies {
				var real, imag float64
				for n, sample := range frame {
					angle := 2 * math.Pi * float64(freq*n) / 8000
					real += float64(sample) * math.Cos(angle)
					imag += float64(sample) * math.Sin(angle)
				}
				powers[i] = math.Hypot(real, imag) / float64(len(frame))
			}
			index := slices.Index(powers, slices.Max(powers))
			return index, powers[index]
		}
		row, lp := strongest(low)
		col, hp := strongest(high)
		var digit byte
		if lp > 300 && hp > 300 {
			digit = keypad[row*4+col]
		}
		if digit == previous {
			run++
		} else {
			previous = digit
			run = 1
		}
		if digit != 0 && run == 5 {
			result = append(result, digit)
		}
	}
	return string(result)
}

func TestInBandDTMFDoesNotRequireTelephoneEventNegotiation(t *testing.T) {
	h := newPipelineHarness(t, 48000)
	c := audioCodecByName(t, g711.ULawSDPNameOnly)
	h.configureType(c, 8000, testAudioPT(c), 0, false, "audio")
	h.drainRemote()
	sent := make(chan error, 1)
	go func() { sent <- h.dtmfOut.WriteSample("5") }()
	var pcm msdk.PCM16Sample
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		packet, ok := h.readRemotePacket(20 * time.Millisecond)
		if ok {
			require.Equal(t, h.audio.Type, packet.PayloadType)
			pcm = append(pcm, g711.ULawSample(packet.Payload).Decode()...)
		}
	}
	require.NoError(t, <-sent)
	require.Equal(t, "5", decodeTestDTMF(pcm))
}
