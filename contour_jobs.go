package main

import (
	"context"
	"errors"
	"fmt"
)

// All job/cache fields are protected by studio.Mutex. Raster/result snapshots
// are immutable, allowing another DTM edit while contour work is in progress.
func (s *studio) invalidateContours(clearCache bool) {
	if s.contourCancel != nil {
		s.contourCancel()
		s.contourCancel = nil
	}
	s.contourJob++
	s.contourBusy = false
	s.contours = nil
	if clearCache {
		s.contourCache = nil
		s.contourDirty = cellRect{}
	}
}

func (s *studio) startContourJob(p ContourParams) error {
	if e := p.validate(); e != nil {
		return e
	}
	s.Lock()
	defer s.Unlock()
	if s.busy || s.result == nil {
		return errors.New("create/open a DTM and wait for terrain extraction")
	}
	s.invalidateContours(false)
	ctx, cancel := context.WithCancel(context.Background())
	s.contourCancel = cancel
	s.contourBusy = true
	s.failure = ""
	s.progress = 0
	s.message = "Refreshing contours; terrain editing remains available"
	job := s.contourJob
	g, z, flags, revision := s.grid, s.result.DTM, s.result.Flags, s.edits.Revision
	prior, dirty := s.contourCache, s.contourDirty
	go func() {
		defer cancel()
		c, cache, e := generateContoursCached(ctx, g, z, p, revision, func(n int, m string) {
			s.Lock()
			defer s.Unlock()
			if s.contourJob == job {
				s.progress = n
				s.message = m
			}
		}, prior, dirty, flags)
		s.Lock()
		defer s.Unlock()
		if s.contourJob != job || s.grid != g || s.edits.Revision != revision {
			return
		}
		s.contourBusy = false
		s.contourCancel = nil
		if e != nil {
			if !errors.Is(e, context.Canceled) {
				s.failure = e.Error()
			}
			s.message = "Contour refresh stopped"
			return
		}
		c.Generation = job
		c.Constraints = append([]ContourConstraint{}, s.constraints...)
		s.contours = c
		s.contourCache = cache
		s.contourDirty = cellRect{}
		s.progress = 100
		s.message = fmt.Sprintf("Contours ready: %d lines · %.0f ms (%s)", len(c.Lines), c.Performance.TotalMilliseconds, c.Performance.Mode)
	}()
	return nil
}
