// Copyright 2026 LiveKit, Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDTMFModeValidation(t *testing.T) {
	for _, mode := range []string{"", "rtp", "audio", "both"} {
		c := Config{DTMFMode: mode}
		require.NoError(t, c.Init())
	}
	c := Config{DTMFMode: "unknown"}
	require.ErrorContains(t, c.Init(), "dtmf_mode")
}
