// Copyright 2026 LiveKit, Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package sip

import (
	"github.com/livekit/protocol/logger"
	"sync"
	"testing"
)

func TestOutboundLoggerEnrichmentDuringRemoteHangup(t *testing.T) {
	base := logger.NewTestLogger(t)
	call := &outboundCall{log: base}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			call.setLogger(base.WithValues("iteration", i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			if call.getLogger() == nil {
				t.Error("hangup callback lost its call logger")
			}
		}
	}()
	wg.Wait()
}
