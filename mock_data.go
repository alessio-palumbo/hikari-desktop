package main

import "fmt"

func MockDeviceSnapshot() DeviceSnapshot {
	return DeviceSnapshot{
		Locations: []Location{
			{ID: "home", Name: "Home"},
			{ID: "studio", Name: "Studio"},
		},
		Groups: []Group{
			{ID: "living", LocationID: "home", Name: "Living Room"},
			{ID: "kitchen", LocationID: "home", Name: "Kitchen"},
			{ID: "desk", LocationID: "studio", Name: "Desk"},
		},
		Devices: []Device{
			{
				ID: "lr-ceiling", GroupID: "living", Serial: serial(0x01a2c3), Name: "Ceiling",
				Model: "A19 color", Kind: "single", Online: true, On: true, Brightness: 0.62,
				Color: &HSLColor{H: 38, S: 0.35, L: 0.65}, Kelvin: 3200,
			},
			{
				ID: "lr-sofa", GroupID: "living", Serial: serial(0x01a2d8), Name: "Sofa Lamp",
				Model: "BR30 color", Kind: "single", Online: true, On: true, Brightness: 0.48,
				Color: &HSLColor{H: 18, S: 0.85, L: 0.55}, Kelvin: 2700,
			},
			{
				ID: "lr-tv", GroupID: "living", Serial: serial(0x01a2e1), Name: "TV Backlight",
				Model: "Z 32", Kind: "multizone", Online: true, On: true, Brightness: 0.78,
				Zones: makeZones(32, 290, 70),
			},
			{
				ID: "lr-tiles", GroupID: "living", Serial: serial(0x01a2e4), Name: "Wall Tiles",
				Model: "Tile 5", Kind: "matrix", Online: true, On: true, Brightness: 0.55,
				Tiles: makeTileChain([]tileSpec{
					{x: 0, y: 0, w: 8, h: 8},
					{x: 8, y: 0, w: 8, h: 8},
					{x: 16, y: 0, w: 8, h: 8},
					{x: 4, y: 8, w: 8, h: 8},
					{x: 12, y: 8, w: 8, h: 8},
				}, 170, 290),
			},
			{
				ID: "kt-pendant", GroupID: "kitchen", Serial: serial(0x02b101), Name: "Pendant",
				Model: "A19 color", Kind: "single", Online: true, On: true, Brightness: 0.9,
				Color: &HSLColor{H: 38, S: 0.2, L: 0.85}, Kelvin: 4500,
			},
			{
				ID: "kt-under", GroupID: "kitchen", Serial: serial(0x02b110), Name: "Under-counter",
				Model: "Z 24", Kind: "multizone", Online: true, On: false, Brightness: 0.55,
				Zones: makeZones(24, 30, 60),
			},
			{
				ID: "of-desk", GroupID: "desk", Serial: serial(0x10f501), Name: "Desk Strip",
				Model: "Z 32", Kind: "multizone", Online: true, On: true, Brightness: 0.85,
				Zones: makeZones(32, 200, 260),
			},
		},
	}
}

func serial(n int) string {
	return fmt.Sprintf("d0:73:d5:%02x:%02x:%02x", (n>>16)&0xff, (n>>8)&0xff, n&0xff)
}

func makeZones(n int, h1 float64, h2 float64) []HSLColor {
	zones := make([]HSLColor, n)
	for i := range zones {
		t := float64(i) / float64(n-1)
		zones[i] = HSLColor{H: h1 + (h2-h1)*t, S: 0.85, L: 0.55}
	}
	return zones
}

type tileSpec struct {
	x, y float64
	w, h int
	rows []TileRow
}

func makeTileChain(layout []tileSpec, h1 float64, h2 float64) []Tile {
	tiles := make([]Tile, 0, len(layout))
	for i, spec := range layout {
		rows := spec.rows
		if rows == nil {
			rows = make([]TileRow, spec.h)
			for r := range rows {
				rows[r] = TileRow{Cols: spec.w}
			}
		}

		pixels := make([]HSLColor, 0)
		for ri, row := range rows {
			for ci := 0; ci < row.Cols; ci++ {
				t := float64(i*8+ri+ci) / 48
				pixels = append(pixels, HSLColor{H: h1 + (h2-h1)*t, S: 0.75, L: 0.5})
			}
		}

		tiles = append(tiles, Tile{
			ID: i, X: spec.x, Y: spec.y, W: float64(spec.w), H: float64(len(rows)),
			Rows: rows, Pixels: pixels,
		})
	}
	return tiles
}
