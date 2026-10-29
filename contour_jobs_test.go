package main

import (
	"net/http/httptest"
	"testing"
)

func TestContourCancellationInvalidatesCurrentJob(t *testing.T) {
	cancelled := false
	s := &studio{
		desktop: true, token: "test", contourBusy: true, contourJob: 7,
		contourCancel: func() { cancelled = true },
		progress:      65, contours: &ContourSet{Generation: 7},
	}
	req := httptest.NewRequest("POST", "/api/cancel", nil)
	req.Header.Set("X-Studio-Token", "test")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 || !cancelled || s.contourBusy || s.contourCancel != nil || s.contours != nil || s.contourJob != 8 {
		t.Fatalf("cancel did not stop and invalidate the contour job: %d %s", w.Code, w.Body.String())
	}
	if s.progress != 0 || s.message != "Contour refresh cancelled" {
		t.Fatal("cancellation left the old progress/status visible")
	}
}
