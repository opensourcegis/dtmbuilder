package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Desktop exposes file dialogs through Wails' in-process Go/JavaScript bridge.
// Raster processing stays in the same Go process; no TCP server is started.
type Desktop struct {
	ctx context.Context
	s   *studio
}

func runDesktop(s *studio) error {
	app := &Desktop{s: s}
	return wails.Run(&options.App{
		Title: "Terrain Studio GIS · Live Contours · UI Update", Width: 1550, Height: 1000, MinWidth: 1150, MinHeight: 740,
		BackgroundColour: &options.RGBA{R: 245, G: 247, B: 249, A: 255},
		AssetServer:      &assetserver.Options{Handler: s}, Bind: []interface{}{app},
		OnStartup: func(ctx context.Context) { app.ctx = ctx },
		OnBeforeClose: func(ctx context.Context) bool {
			s.Lock()
			unsaved := s.result != nil && s.result.Counts["manual"] > 0 && s.edits.Revision != s.edits.SavedRevision
			s.Unlock()
			if !unsaved {
				return false
			}
			answer, e := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{Type: wruntime.QuestionDialog, Title: "Unsaved terrain edits", Message: "Close without saving your manually edited DTM?", Buttons: []string{"Yes", "No"}, DefaultButton: "No", CancelButton: "No"})
			return e != nil || answer != "Yes"
		},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "terrain-studio-gis-live-contours-v3", OnSecondInstanceLaunch: func(options.SecondInstanceData) { wruntime.WindowUnminimise(app.ctx); wruntime.WindowShow(app.ctx) }},
		OnShutdown: func(context.Context) {
			s.Lock()
			defer s.Unlock()
			if s.cancel != nil {
				s.cancel()
			}
			if s.contourCancel != nil {
				s.contourCancel()
			}
		},
	})
}

func (d *Desktop) OpenDSM() (map[string]any, error) {
	return d.openRaster(false)
}
func (d *Desktop) OpenDTM() (map[string]any, error) { return d.openRaster(true) }
func (d *Desktop) openRaster(asDTM bool) (map[string]any, error) {
	d.s.Lock()
	busy := d.s.busy
	d.s.Unlock()
	if busy {
		return nil, errors.New("wait for processing to finish")
	}
	path, e := wruntime.OpenFileDialog(d.ctx, wruntime.OpenDialogOptions{Title: "Open elevation DSM", Filters: []wruntime.FileFilter{{DisplayName: "Elevation GeoTIFF", Pattern: "*.tif;*.tiff"}}})
	if e != nil || path == "" {
		return nil, e
	}
	g, e := readTIFF(path)
	if e != nil {
		return nil, e
	}
	d.s.Lock()
	defer d.s.Unlock()
	if d.s.busy {
		return nil, errors.New("a job is running")
	}
	d.s.grid = g
	d.s.name = filepath.Base(path)
	d.s.result = nil
	d.s.resetEdits()
	if asDTM {
		d.s.result = initializeDTM(g)
		d.s.params = defaults()
		d.s.resultMethod = "Imported DTM"
	}
	d.s.failure = ""
	d.s.progress = 0
	d.s.message = "Elevation raster loaded"
	return d.s.state(), nil
}

func (d *Desktop) Export(layer string) (string, error) {
	d.s.Lock()
	g, z := d.s.layer(layer)
	revision := d.s.edits.Revision
	if d.s.busy {
		d.s.Unlock()
		return "", errors.New("wait for processing to finish")
	}
	name := safeLayer(layer) + ".tif"
	var report []byte
	if layer == "report" && d.s.result != nil {
		name = "terrain-report.json"
		report, _ = json.MarshalIndent(map[string]any{"input": d.s.name, "parameters": d.s.params, "counts": d.s.result.Counts, "edits": d.s.edits.Audit, "method": d.s.resultMethod, "flags": "1 low, 2 high, 4 spike, 8 non-ground, 16 original NoData, 32 filled output, 64 manual edit, 128 hydroflatten, 256 contour pin; bitwise combined", "contours": contourReport(d.s.grid, d.s.contours), "constraints": d.s.constraints}, "", "  ")
	}
	d.s.Unlock()
	if report == nil && (g == nil || z == nil) {
		return "", errors.New("run processing first")
	}
	pattern := "*.tif"
	if report != nil {
		pattern = "*.json"
	}
	dest, e := wruntime.SaveFileDialog(d.ctx, wruntime.SaveDialogOptions{Title: "Save " + name, DefaultFilename: name, Filters: []wruntime.FileFilter{{DisplayName: "Output file", Pattern: pattern}}})
	if e != nil || dest == "" {
		return "", e
	}
	if !d.confirmOverwrite(dest) {
		return "", nil
	}
	b := report
	if b == nil {
		b, e = encodeTIFF(g, z)
		if e != nil {
			return "", e
		}
	}
	if e = os.WriteFile(dest, b, 0644); e != nil {
		return "", e
	}
	if layer == "dtm" {
		d.s.Lock()
		if d.s.edits.Revision == revision {
			d.s.edits.SavedRevision = revision
		}
		d.s.Unlock()
	}
	return dest, nil
}
func (d *Desktop) SaveSettings(p Params) (string, error) {
	if e := p.validate(); e != nil {
		return "", e
	}
	dest, e := wruntime.SaveFileDialog(d.ctx, wruntime.SaveDialogOptions{Title: "Save terrain settings", DefaultFilename: "terrain-settings.json", Filters: []wruntime.FileFilter{{DisplayName: "JSON settings", Pattern: "*.json"}}})
	if e != nil || dest == "" {
		return "", e
	}
	if !d.confirmOverwrite(dest) {
		return "", nil
	}
	b, e := json.MarshalIndent(p, "", "  ")
	if e != nil {
		return "", e
	}
	return dest, os.WriteFile(dest, b, 0644)
}
func (d *Desktop) LoadSettings() (*Params, error) {
	path, e := wruntime.OpenFileDialog(d.ctx, wruntime.OpenDialogOptions{Title: "Load terrain settings", Filters: []wruntime.FileFilter{{DisplayName: "JSON settings", Pattern: "*.json"}}})
	if e != nil || path == "" {
		return nil, e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	p := defaults()
	if e = json.Unmarshal(b, &p); e != nil {
		return nil, e
	}
	if e = p.validate(); e != nil {
		return nil, e
	}
	return &p, nil
}
func (d *Desktop) confirmOverwrite(path string) bool {
	if _, e := os.Stat(path); os.IsNotExist(e) {
		return true
	}
	answer, e := wruntime.MessageDialog(d.ctx, wruntime.MessageDialogOptions{Type: wruntime.QuestionDialog, Title: "Replace existing file?", Message: "Replace " + path + "?", Buttons: []string{"Yes", "No"}, DefaultButton: "No", CancelButton: "No"})
	return e == nil && answer == "Yes"
}

func (d *Desktop) ExportContours(surface bool) (string, error) {
	d.s.Lock()
	g, c := d.s.grid, d.s.contours
	d.s.Unlock()
	if c == nil {
		return "", errors.New("generate contours first")
	}
	filename, pattern := "contours.csv", "*.csv"
	if surface {
		filename, pattern = "contour-surface.tif", "*.tif"
	}
	path, e := wruntime.SaveFileDialog(d.ctx, wruntime.SaveDialogOptions{Title: "Save " + filename, DefaultFilename: filename, Filters: []wruntime.FileFilter{{DisplayName: "Contour output", Pattern: pattern}}})
	if e != nil || path == "" {
		return "", e
	}
	if !d.confirmOverwrite(path) {
		return "", nil
	}
	var b []byte
	if surface {
		b, e = encodeTIFF(c.Surface, c.Surface.Z)
	} else {
		b, e = contourCSV(g, c)
	}
	if e != nil {
		return "", e
	}
	return path, os.WriteFile(path, b, 0644)
}

func (d *Desktop) ExportContourReport() (string, error) {
	d.s.Lock()
	if d.s.contours == nil || d.s.busy {
		d.s.Unlock()
		return "", errors.New("generate contours and wait for processing to finish")
	}
	b, e := json.MarshalIndent(contourReport(d.s.grid, d.s.contours), "", "  ")
	d.s.Unlock()
	if e != nil {
		return "", e
	}
	path, e := wruntime.SaveFileDialog(d.ctx, wruntime.SaveDialogOptions{Title: "Save contour quality report", DefaultFilename: "contour-report.json", Filters: []wruntime.FileFilter{{DisplayName: "JSON report", Pattern: "*.json"}}})
	if e != nil || path == "" {
		return "", e
	}
	if !d.confirmOverwrite(path) {
		return "", nil
	}
	return path, os.WriteFile(path, b, 0644)
}

func (d *Desktop) ExportContourGPKG() (string, error) {
	d.s.Lock()
	g, c := d.s.grid, d.s.contours
	d.s.Unlock()
	if c == nil {
		return "", errors.New("wait for current contours to finish refreshing")
	}
	path, e := wruntime.SaveFileDialog(d.ctx, wruntime.SaveDialogOptions{Title: "Save contour GeoPackage", DefaultFilename: "contours.gpkg", Filters: []wruntime.FileFilter{{DisplayName: "GeoPackage", Pattern: "*.gpkg"}}})
	if e != nil || path == "" {
		return "", e
	}
	if !d.confirmOverwrite(path) {
		return "", nil
	}
	b, e := contourGPKG(d.ctx, g, c)
	if e != nil {
		return "", e
	}
	return path, os.WriteFile(path, b, 0644)
}
