// Copyright 2026 LiveKit, Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMixerInputBufferBounds(t *testing.T) {
	for _, frames := range []int{0, 1, 5, 15, 50} {
		c := Config{MixerInputBufferFrames: frames}
		require.NoError(t, c.Init())
	}
	for _, frames := range []int{-1, 51} {
		c := Config{MixerInputBufferFrames: frames}
		require.ErrorContains(t, c.Init(), "mixer_input_buffer_frames")
	}
}
