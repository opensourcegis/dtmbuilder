package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

const maxCells = 16000000

type tag struct {
	ID, Type uint16
	Count    uint32
	Data     []byte
}
type Grid struct {
	W, H      int
	Z         []float32
	Tags      map[uint16]tag
	Order     binary.ByteOrder
	PixelSize float64
}

var sizes = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8}

func readTIFF(path string) (*Grid, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > 512<<20 {
		return nil, errors.New("input exceeds the 512 MiB file limit; crop or resample first")
	}
	b, err := io.ReadAll(io.LimitReader(f, 512<<20+1))
	if err != nil {
		return nil, err
	}
	return decodeTIFF(b)
}
func decodeTIFF(b []byte) (*Grid, error) {
	if len(b) < 8 {
		return nil, errors.New("file is too short for TIFF")
	}
	var bo binary.ByteOrder
	switch string(b[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, errors.New("expected a GeoTIFF elevation raster")
	}
	if bo.Uint16(b[2:4]) != 42 {
		return nil, errors.New("BigTIFF is not supported; export a cropped classic GeoTIFF")
	}
	off := uint64(bo.Uint32(b[4:8]))
	if off+2 > uint64(len(b)) {
		return nil, errors.New("invalid TIFF directory")
	}
	nt := int(bo.Uint16(b[off : off+2]))
	if off+2+uint64(nt*12)+4 > uint64(len(b)) {
		return nil, errors.New("truncated TIFF directory")
	}
	tags := map[uint16]tag{}
	for i := 0; i < nt; i++ {
		e := b[int(off)+2+i*12 : int(off)+14+i*12]
		id, t, c := bo.Uint16(e), bo.Uint16(e[2:]), bo.Uint32(e[4:])
		size := uint64(sizes[t]) * uint64(c)
		if size == 0 {
			continue
		}
		var d []byte
		if size <= 4 {
			d = append([]byte(nil), e[8:8+int(size)]...)
		} else {
			p := uint64(bo.Uint32(e[8:]))
			if p+size > uint64(len(b)) {
				return nil, errors.New("TIFF tag outside file")
			}
			d = append([]byte(nil), b[p:p+size]...)
		}
		tags[id] = tag{id, t, c, d}
	}
	values := func(id uint16) []int {
		t := tags[id]
		out := make([]int, t.Count)
		for i := range out {
			switch t.Type {
			case 3:
				out[i] = int(bo.Uint16(t.Data[2*i:]))
			case 4:
				out[i] = int(bo.Uint32(t.Data[4*i:]))
			case 1:
				out[i] = int(t.Data[i])
			}
		}
		return out
	}
	val := func(id uint16, def int) int {
		v := values(id)
		if len(v) == 0 {
			return def
		}
		return v[0]
	}
	w, h := val(256, 0), val(257, 0)
	if w < 1 || h < 1 || w > maxCells || h > maxCells || uint64(w)*uint64(h) > maxCells {
		return nil, errors.New("raster must contain 1–16 million cells; crop/resample larger rasters")
	}
	if val(277, 1) != 1 || val(262, 1) == 2 || val(262, 1) == 3 {
		return nil, errors.New("RGB/multiband imagery detected. Select a single-band elevation DSM; colour values cannot produce terrain heights")
	}
	bits, format, compression, predictor := val(258, 8), val(339, 1), val(259, 1), val(317, 1)
	if (bits != 8 && bits != 16 && bits != 32 && bits != 64) || format < 1 || format > 3 || (format == 3 && bits != 32 && bits != 64) {
		return nil, errors.New("unsupported elevation sample type")
	}
	if predictor < 1 || predictor > 3 || predictor == 3 && format != 3 {
		return nil, errors.New("unsupported TIFF predictor")
	}
	nodata := math.NaN()
	if t, ok := tags[42113]; ok {
		nodata, _ = strconv.ParseFloat(strings.TrimRight(string(t.Data), "\x00 "), 64)
	}
	z := make([]float32, w*h)
	for i := range z {
		z[i] = float32(math.NaN())
	}
	bw, bh := w, val(278, h)
	offsets, counts := values(273), values(279)
	tiled := false
	if _, ok := tags[324]; ok {
		bw, bh = val(322, 0), val(323, 0)
		offsets, counts = values(324), values(325)
		tiled = true
	}
	if bw < 1 || bh < 1 || bw > maxCells || bh > maxCells || uint64(bw)*uint64(bh) > maxCells || len(offsets) != len(counts) {
		return nil, errors.New("invalid TIFF block layout")
	}
	across := (w + bw - 1) / bw
	expected := ((h + bh - 1) / bh) * across
	if !tiled {
		expected = (h + bh - 1) / bh
	}
	if len(offsets) != expected {
		return nil, errors.New("missing TIFF blocks")
	}
	bps := bits / 8
	for block, p := range offsets {
		sz := counts[block]
		if p < 0 || sz < 0 || uint64(p)+uint64(sz) > uint64(len(b)) {
			return nil, errors.New("TIFF block outside file")
		}
		rows := bh
		bx, by := (block%across)*bw, (block/across)*bh
		if !tiled {
			bx = 0
			by = block * bh
			rows = min(bh, h-by)
		}
		want := bw * rows * bps
		var raw []byte
		switch compression {
		case 1:
			raw = b[p : p+sz]
		case 8, 32946:
			r, e := zlib.NewReader(bytes.NewReader(b[p : p+sz]))
			if e != nil {
				return nil, e
			}
			raw, e = io.ReadAll(io.LimitReader(r, int64(want)+1))
			r.Close()
			if e != nil {
				return nil, e
			}
		case 5:
			var e error
			raw, e = tiffLZW(b[p:p+sz], want)
			if e != nil {
				return nil, e
			}
		default:
			return nil, fmt.Errorf("TIFF compression %d unsupported; use uncompressed, Deflate or LZW", compression)
		}
		if len(raw) != want {
			return nil, fmt.Errorf("TIFF block size mismatch: got %d expected %d", len(raw), want)
		}
		if predictor > 1 {
			raw = append([]byte(nil), raw...)
		}
		for y := 0; y < rows; y++ {
			row := raw[y*bw*bps : (y+1)*bw*bps]
			if predictor == 2 {
				for x := 1; x < bw; x++ {
					a, c := row[x*bps:], row[(x-1)*bps:]
					switch bps {
					case 1:
						a[0] += c[0]
					case 2:
						bo.PutUint16(a, bo.Uint16(a)+bo.Uint16(c))
					case 4:
						bo.PutUint32(a, bo.Uint32(a)+bo.Uint32(c))
					case 8:
						bo.PutUint64(a, bo.Uint64(a)+bo.Uint64(c))
					}
				}
			}
			if predictor == 3 {
				for k := 1; k < len(row); k++ {
					row[k] += row[k-1]
				}
				unshuffled := make([]byte, len(row))
				for x := 0; x < bw; x++ {
					for k := 0; k < bps; k++ {
						plane := k
						if bo == binary.LittleEndian {
							plane = bps - 1 - k
						}
						unshuffled[x*bps+k] = row[plane*bw+x]
					}
				}
				row = unshuffled
			}
			if by+y >= h {
				continue
			}
			for x := 0; x < bw && bx+x < w; x++ {
				q := row[x*bps:]
				v := 0.0
				switch bits {
				case 8:
					if format == 2 {
						v = float64(int8(q[0]))
					} else {
						v = float64(q[0])
					}
				case 16:
					if format == 2 {
						v = float64(int16(bo.Uint16(q)))
					} else {
						v = float64(bo.Uint16(q))
					}
				case 32:
					if format == 3 {
						v = float64(math.Float32frombits(bo.Uint32(q)))
					} else if format == 2 {
						v = float64(int32(bo.Uint32(q)))
					} else {
						v = float64(bo.Uint32(q))
					}
				case 64:
					if format == 3 {
						v = math.Float64frombits(bo.Uint64(q))
					} else if format == 2 {
						v = float64(int64(bo.Uint64(q)))
					} else {
						v = float64(bo.Uint64(q))
					}
				}
				if v == nodata || math.IsNaN(v) || math.IsInf(v, 0) {
					continue
				}
				z[(by+y)*w+bx+x] = float32(v)
			}
		}
	}
	validCount := 0
	for _, v := range z {
		if valid(v) {
			validCount++
		}
	}
	if validCount == 0 {
		return nil, errors.New("raster contains no valid elevation samples")
	}
	pixel := 0.0
	if t, ok := tags[33550]; ok && len(t.Data) >= 16 {
		pixel = math.Float64frombits(bo.Uint64(t.Data))
	}
	return &Grid{w, h, z, tags, bo, pixel}, nil
}

// TIFF LZW uses MSB codes and early code-width changes (unlike GIF LZW).
func tiffLZW(src []byte, limit int) ([]byte, error) {
	dict := make([][]byte, 4096)
	reset := func() {
		for i := 0; i < 256; i++ {
			dict[i] = []byte{byte(i)}
		}
		for i := 256; i < 4096; i++ {
			dict[i] = nil
		}
	}
	reset()
	width, next, pos := 9, 258, 0
	var prev []byte
	out := make([]byte, 0, limit)
	for {
		if pos+width > len(src)*8 {
			return nil, errors.New("truncated LZW stream")
		}
		code := 0
		for k := 0; k < width; k++ {
			code = code<<1 | int((src[(pos+k)/8]>>uint(7-(pos+k)%8))&1)
		}
		pos += width
		if code == 256 {
			reset()
			width, next = 9, 258
			prev = nil
			continue
		}
		if code == 257 {
			break
		}
		var entry []byte
		if code < next && dict[code] != nil {
			entry = dict[code]
		} else if code == next && len(prev) > 0 {
			entry = append(append([]byte(nil), prev...), prev[0])
		} else {
			return nil, errors.New("invalid LZW code")
		}
		if len(out)+len(entry) > limit {
			return nil, errors.New("LZW data exceeds block size")
		}
		out = append(out, entry...)
		if len(prev) > 0 && next < 4096 {
			dict[next] = append(append([]byte(nil), prev...), entry[0])
			next++
			if next == (1<<width)-1 && width < 12 {
				width++
			}
		}
		prev = entry
	}
	return out, nil
}

func encodeTIFF(g *Grid, z []float32) ([]byte, error) {
	if len(z) != g.W*g.H {
		return nil, errors.New("output size mismatch")
	}
	bo := g.Order
	if bo == nil {
		bo = binary.LittleEndian
	}
	tags := map[uint16]tag{}
	set := func(id, t uint16, v uint32) {
		d := make([]byte, sizes[t])
		if t == 3 {
			bo.PutUint16(d, uint16(v))
		} else {
			bo.PutUint32(d, v)
		}
		tags[id] = tag{id, t, 1, d}
	}
	set(256, 4, uint32(g.W))
	set(257, 4, uint32(g.H))
	set(258, 3, 32)
	set(259, 3, 1)
	set(262, 3, 1)
	set(273, 4, 0)
	set(277, 3, 1)
	set(278, 4, uint32(g.H))
	set(279, 4, uint32(len(z)*4))
	set(339, 3, 3)
	tags[42113] = tag{42113, 2, 4, []byte("nan\x00")}
	for _, id := range []uint16{33550, 33922, 34264, 34735, 34736, 34737} {
		if t, ok := g.Tags[id]; ok {
			tags[id] = t
		}
	}
	ids := []int{}
	for id := range tags {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	off := 8 + 2 + len(ids)*12 + 4
	for _, id := range ids {
		t := tags[uint16(id)]
		if len(t.Data) > 4 {
			off += len(t.Data)
			if off%2 != 0 {
				off++
			}
		}
	}
	set(273, 4, uint32(off))
	out := make([]byte, off+len(z)*4)
	if bo == binary.LittleEndian {
		copy(out, "II")
	} else {
		copy(out, "MM")
	}
	bo.PutUint16(out[2:], 42)
	bo.PutUint32(out[4:], 8)
	bo.PutUint16(out[8:], uint16(len(ids)))
	extra := 8 + 2 + len(ids)*12 + 4
	for i, id := range ids {
		t := tags[uint16(id)]
		e := out[10+i*12:]
		bo.PutUint16(e, t.ID)
		bo.PutUint16(e[2:], t.Type)
		bo.PutUint32(e[4:], t.Count)
		if len(t.Data) <= 4 {
			copy(e[8:12], t.Data)
		} else {
			bo.PutUint32(e[8:], uint32(extra))
			copy(out[extra:], t.Data)
			extra += len(t.Data)
			if extra%2 != 0 {
				extra++
			}
		}
	}
	for i, v := range z {
		bo.PutUint32(out[off+i*4:], math.Float32bits(v))
	}
	return out, nil
}
