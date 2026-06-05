package backend

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

func TestLifxTransportSnapshotMapsGetDevices(t *testing.T) {
	serial, err := lifxdevice.SerialFromHex("d073d501a2c3")
	if err != nil {
		t.Fatalf("SerialFromHex returned error: %v", err)
	}
	dev := lifxdevice.Device{
		Address:      &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 56700},
		Serial:       serial,
		Label:        "Desk Strip",
		RegistryName: "LIFX Z",
		Location:     "Studio",
		Group:        "Desk",
		PoweredOn:    true,
		Color: lifxdevice.Color{
			Hue:        200,
			Saturation: 80,
			Brightness: 60,
			Kelvin:     3500,
		},
		MultizoneProperties: lifxdevice.MultizoneProperties{
			Zones: []packets.LightHsbk{
				{Hue: lifxdevice.ConvertExternalToDeviceValue(10, 360), Saturation: lifxdevice.ConvertExternalToDeviceValue(90, 100), Brightness: lifxdevice.ConvertExternalToDeviceValue(50, 100), Kelvin: 3500},
			},
		},
	}
	dev.SetProductInfo(31)

	controller := &fakeLifxController{devices: []lifxdevice.Device{dev}}
	transport := NewLifxTransportWithController(controller)

	snapshot, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if len(snapshot.Locations) != 1 || snapshot.Locations[0].Name != "Studio" {
		t.Fatalf("locations = %#v", snapshot.Locations)
	}
	if len(snapshot.Groups) != 1 || snapshot.Groups[0].Name != "Desk" {
		t.Fatalf("groups = %#v", snapshot.Groups)
	}
	if len(snapshot.Devices) != 1 {
		t.Fatalf("devices = %#v", snapshot.Devices)
	}
	got := snapshot.Devices[0]
	if got.Serial != "d073d501a2c3" || got.Name != "Desk Strip" || got.Kind != "multizone" {
		t.Fatalf("device = %#v", got)
	}
	if got.Brightness != 0.6 {
		t.Fatalf("brightness = %v, want 0.6", got.Brightness)
	}
	if !got.Capability.HasColor || got.Capability.KelvinMin != 2500 || got.Capability.KelvinMax != 9000 {
		t.Fatalf("capability = %#v, want color with 2500-9000K", got.Capability)
	}
	if len(got.Zones) != 1 || got.Zones[0].L != 0.5 {
		t.Fatalf("zones = %#v", got.Zones)
	}
}

func TestLifxTransportSnapshotMapsEmptyDiscoveryAsEmptyArrays(t *testing.T) {
	snapshot := mapLifxDevices(nil)
	if snapshot.Locations == nil || snapshot.Groups == nil || snapshot.Devices == nil {
		t.Fatalf("snapshot contains nil slices: %#v", snapshot)
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	want := `{"locations":[],"groups":[],"devices":[]}`
	if string(payload) != want {
		t.Fatalf("json = %s, want %s", payload, want)
	}
}

func TestLifxTransportSnapshotFiltersSwitchDevices(t *testing.T) {
	switchSerial, err := lifxdevice.SerialFromHex("d073d501a2c4")
	if err != nil {
		t.Fatalf("SerialFromHex returned error: %v", err)
	}
	switchDevice := lifxdevice.Device{
		Serial:   switchSerial,
		Label:    "Wall Switch",
		Location: "Studio",
		Group:    "Desk",
	}
	switchDevice.SetProductInfo(70)

	hybridSerial, err := lifxdevice.SerialFromHex("d073d501a2c5")
	if err != nil {
		t.Fatalf("SerialFromHex returned error: %v", err)
	}
	hybridDevice := lifxdevice.Device{
		Serial:    hybridSerial,
		Label:     "Everyday Strip",
		Location:  "Studio",
		Group:     "Desk",
		PoweredOn: true,
		Color:     lifxdevice.Color{Brightness: 50, Kelvin: 3500},
	}
	hybridDevice.SetProductInfo(207)

	snapshot := mapLifxDevices([]lifxdevice.Device{switchDevice, hybridDevice})
	if len(snapshot.Devices) != 1 {
		t.Fatalf("devices = %#v, want only hybrid light", snapshot.Devices)
	}
	if snapshot.Devices[0].Serial != "d073d501a2c5" {
		t.Fatalf("device serial = %s, want hybrid serial", snapshot.Devices[0].Serial)
	}
	if snapshot.Devices[0].Kind != "multizone" {
		t.Fatalf("device kind = %s, want multizone", snapshot.Devices[0].Kind)
	}
}

func TestLifxTransportSnapshotMapsFixedKelvinAsWhite(t *testing.T) {
	serial, err := lifxdevice.SerialFromHex("d073d501a2c6")
	if err != nil {
		t.Fatalf("SerialFromHex returned error: %v", err)
	}
	dev := lifxdevice.Device{
		Serial:    serial,
		Label:     "Warm White",
		Location:  "Studio",
		Group:     "Desk",
		PoweredOn: true,
		Color: lifxdevice.Color{
			Hue:        240,
			Saturation: 100,
			Brightness: 60,
			Kelvin:     2000,
		},
		ColorProperties: lifxdevice.ColorProperties{
			HasColor:         false,
			TemperatureRange: lifxdevice.TemperatureRange{Min: 2000, Max: 2000},
		},
	}

	snapshot := mapLifxDevices([]lifxdevice.Device{dev})
	if len(snapshot.Devices) != 1 {
		t.Fatalf("devices = %#v", snapshot.Devices)
	}
	got := snapshot.Devices[0]
	if got.Kelvin != 2000 {
		t.Fatalf("kelvin = %d, want 2000", got.Kelvin)
	}
	if got.Color == nil || got.Color.S != 0 || got.Color.Kelvin != 2000 {
		t.Fatalf("color = %#v, want saturation-zero 2000K white", got.Color)
	}
}

func TestLifxTransportSnapshotMapsCandleAsIrregularMatrix(t *testing.T) {
	serial, err := lifxdevice.SerialFromHex("d073d501a2c7")
	if err != nil {
		t.Fatalf("SerialFromHex returned error: %v", err)
	}
	zones := make([]packets.LightHsbk, 55)
	for i := range zones {
		zones[i] = packets.LightHsbk{Brightness: lifxdevice.ConvertExternalToDeviceValue(50, 100), Kelvin: 3500}
	}
	dev := lifxdevice.Device{
		Serial:    serial,
		Label:     "Candle",
		Location:  "Studio",
		Group:     "Desk",
		PoweredOn: true,
		Color:     lifxdevice.Color{Brightness: 50, Kelvin: 3500},
		MatrixProperties: lifxdevice.MatrixProperties{
			Width:      5,
			Height:     11,
			NZones:     55,
			ChainZones: [][]packets.LightHsbk{zones},
		},
	}
	dev.SetProductInfo(57)

	snapshot := mapLifxDevices([]lifxdevice.Device{dev})
	if len(snapshot.Devices) != 1 {
		t.Fatalf("devices = %#v", snapshot.Devices)
	}
	matrix := snapshot.Devices[0].Chain[0]
	if len(matrix.Pixels) != 55 {
		t.Fatalf("pixels = %d, want 55", len(matrix.Pixels))
	}
	if len(matrix.Rows) != 11 {
		t.Fatalf("rows = %d, want 11", len(matrix.Rows))
	}
	if got := matrix.Rows[0].HiddenCols; len(got) != 3 || got[0] != 2 || got[1] != 3 || got[2] != 4 {
		t.Fatalf("first row hidden columns = %#v, want [2 3 4]", got)
	}
	if matrix.Rows[0].Offset != 1 {
		t.Fatalf("first row offset = %v, want 1", matrix.Rows[0].Offset)
	}
	for i, row := range matrix.Rows[1:] {
		if len(row.HiddenCols) != 0 {
			t.Fatalf("row %d hidden columns = %#v, want none", i+1, row.HiddenCols)
		}
	}
}

func TestLifxTransportSnapshotMapsCeilingCustomGridProduct(t *testing.T) {
	serial, err := lifxdevice.SerialFromHex("d073d501a2c8")
	if err != nil {
		t.Fatalf("SerialFromHex returned error: %v", err)
	}
	zones := make([]packets.LightHsbk, 64)
	for i := range zones {
		zones[i] = packets.LightHsbk{Brightness: lifxdevice.ConvertExternalToDeviceValue(50, 100), Kelvin: 3500}
	}
	dev := lifxdevice.Device{
		Serial:    serial,
		Label:     "Ceiling",
		Location:  "Studio",
		Group:     "Desk",
		PoweredOn: true,
		Color:     lifxdevice.Color{Brightness: 50, Kelvin: 3500},
		MatrixProperties: lifxdevice.MatrixProperties{
			Width:      8,
			Height:     8,
			NZones:     64,
			ChainZones: [][]packets.LightHsbk{zones},
		},
	}
	dev.SetProductInfo(265)

	snapshot := mapLifxDevices([]lifxdevice.Device{dev})
	if len(snapshot.Devices) != 1 {
		t.Fatalf("devices = %#v", snapshot.Devices)
	}
	rows := snapshot.Devices[0].Chain[0].Rows
	if len(rows[0].HiddenCols) != 4 || rows[0].HiddenCols[0] != 0 || rows[0].HiddenCols[1] != 1 || rows[0].HiddenCols[2] != 6 || rows[0].HiddenCols[3] != 7 {
		t.Fatalf("first row hidden columns = %#v, want [0 1 6 7]", rows[0].HiddenCols)
	}
	if len(rows[7].HiddenCols) != 4 || rows[7].HiddenCols[0] != 0 || rows[7].HiddenCols[1] != 1 || rows[7].HiddenCols[2] != 6 || rows[7].HiddenCols[3] != 7 {
		t.Fatalf("last row hidden columns = %#v, want [0 1 6 7]", rows[7].HiddenCols)
	}
}

func TestLifxTransportSnapshotRendersCeilingCapsuleAsWideGridWhenDeviceReportsTallGrid(t *testing.T) {
	serial, err := lifxdevice.SerialFromHex("d073d501a2c9")
	if err != nil {
		t.Fatalf("SerialFromHex returned error: %v", err)
	}
	zones := make([]packets.LightHsbk, 128)
	for i := range zones {
		zones[i] = packets.LightHsbk{Brightness: lifxdevice.ConvertExternalToDeviceValue(50, 100), Kelvin: 3500}
	}
	dev := lifxdevice.Device{
		Serial:    serial,
		Label:     "Ceiling Capsule",
		Location:  "Studio",
		Group:     "Desk",
		PoweredOn: true,
		Color:     lifxdevice.Color{Brightness: 50, Kelvin: 3500},
		MatrixProperties: lifxdevice.MatrixProperties{
			Width:      8,
			Height:     16,
			NZones:     128,
			ChainZones: [][]packets.LightHsbk{zones},
		},
	}
	dev.SetProductInfo(201)

	snapshot := mapLifxDevices([]lifxdevice.Device{dev})
	matrix := snapshot.Devices[0].Chain[0]
	if matrix.SendWidth != 8 {
		t.Fatalf("send width = %d, want original device width 8", matrix.SendWidth)
	}
	if matrix.W != 16 || matrix.H != 8 {
		t.Fatalf("display size = %vx%v, want 16x8", matrix.W, matrix.H)
	}
	if len(matrix.Rows) != 8 || matrix.Rows[0].Cols != 16 {
		t.Fatalf("rows = %#v, want 8 rows of 16 columns", matrix.Rows)
	}
	if len(matrix.Rows[0].HiddenCols) != 4 || matrix.Rows[0].HiddenCols[2] != 14 || matrix.Rows[0].HiddenCols[3] != 15 {
		t.Fatalf("first row hidden columns = %#v, want [0 1 14 15]", matrix.Rows[0].HiddenCols)
	}
}

func TestLifxTransportSnapshotAppliesMatrixOrientationForPreview(t *testing.T) {
	serial, err := lifxdevice.SerialFromHex("d073d501a2ca")
	if err != nil {
		t.Fatalf("SerialFromHex returned error: %v", err)
	}
	zones := []packets.LightHsbk{
		testHSBK(0),
		testHSBK(10),
		testHSBK(20),
		testHSBK(30),
	}
	dev := lifxdevice.Device{
		Serial:    serial,
		Label:     "Oriented Matrix",
		Location:  "Studio",
		Group:     "Desk",
		PoweredOn: true,
		Color:     lifxdevice.Color{Brightness: 50, Kelvin: 3500},
		MatrixProperties: lifxdevice.MatrixProperties{
			Width:             2,
			Height:            2,
			NZones:            4,
			ChainOrientations: []lifxdevice.Orientation{lifxdevice.OrientationRight},
			ChainZones:        [][]packets.LightHsbk{zones},
		},
	}
	dev.SetProductInfo(55)

	snapshot := mapLifxDevices([]lifxdevice.Device{dev})
	matrix := snapshot.Devices[0].Chain[0]
	if matrix.Orientation != int(lifxdevice.OrientationRight) {
		t.Fatalf("orientation = %d, want %d", matrix.Orientation, lifxdevice.OrientationRight)
	}
	assertPixelHues(t, matrix.Pixels, []float64{20, 0, 30, 10})
}

func TestLifxTransportStartKeepsInjectedController(t *testing.T) {
	controller := &fakeLifxController{}
	transport := NewLifxTransportWithController(controller)

	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("second Start returned error: %v", err)
	}
	if transport.controller != controller {
		t.Fatal("Start replaced injected controller")
	}
}

func TestLifxTransportRequiresStart(t *testing.T) {
	transport := NewLifxTransport()
	if _, err := transport.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot returned nil error, want not started error")
	}
	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: Device{Serial: "d073d501a2c3", Kind: "single"}}); err == nil {
		t.Fatal("SetDeviceState returned nil error, want not started error")
	}
}

func TestLifxTransportSetDeviceStateSendsSingleZonePowerAndColor(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Name:       "Test",
		Kind:       "single",
		On:         true,
		Brightness: 0.42,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 210, S: 0.75, L: 0.6},
		Kelvin:     4000,
	}
	transport := NewLifxTransportWithController(controller)

	got, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device})
	if err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if got.Serial != device.Serial {
		t.Fatalf("SetDeviceState returned %#v, want %#v", got, device)
	}
	if len(controller.sends) != 2 {
		t.Fatalf("sent %d messages, want 2", len(controller.sends))
	}
	if controller.sends[0].serial.String() != device.Serial {
		t.Fatalf("sent serial = %s, want %s", controller.sends[0].serial.String(), device.Serial)
	}
	if _, ok := controller.sends[0].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("first payload = %T, want *packets.DeviceSetPower", controller.sends[0].msg.Payload)
	}
	payload, ok := controller.sends[1].msg.Payload.(*packets.LightSetWaveformOptional)
	if !ok {
		t.Fatalf("second payload = %T, want *packets.LightSetWaveformOptional", controller.sends[1].msg.Payload)
	}
	color := lifxdevice.NewColor(payload.Color)
	if !payload.SetHue || color.Hue != 210 {
		t.Fatalf("hue = %v/%v, want set 210", payload.SetHue, color.Hue)
	}
	if !payload.SetSaturation || color.Saturation != 75 {
		t.Fatalf("saturation = %v/%v, want set 75", payload.SetSaturation, color.Saturation)
	}
	if !payload.SetBrightness || color.Brightness != 42 {
		t.Fatalf("brightness = %v/%v, want set 42", payload.SetBrightness, color.Brightness)
	}
	if !payload.SetKelvin || payload.Color.Kelvin != 4000 {
		t.Fatalf("kelvin = %v/%v, want set 4000", payload.SetKelvin, payload.Color.Kelvin)
	}
}

func TestLifxTransportSetDeviceStateSendsSingleZonePowerOffOnly(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{Serial: "d073d501a2c3", Kind: "single", On: false}
	transport := NewLifxTransportWithController(controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sends) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sends))
	}
	payload, ok := controller.sends[0].msg.Payload.(*packets.DeviceSetPower)
	if !ok {
		t.Fatalf("payload = %T, want *packets.DeviceSetPower", controller.sends[0].msg.Payload)
	}
	if payload.Level != 0 {
		t.Fatalf("power level = %d, want 0", payload.Level)
	}
}

func TestLifxTransportSetDeviceStateClampsWhiteOnlyDevice(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       "single",
		On:         true,
		Brightness: 0.5,
		Capability: DeviceCapability{HasColor: false, KelvinMin: 2700, KelvinMax: 6500},
		Color:      &HSLColor{H: 210, S: 0.75, L: 0.6},
		Kelvin:     9000,
	}
	transport := NewLifxTransportWithController(controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	payload, ok := controller.sends[1].msg.Payload.(*packets.LightSetWaveformOptional)
	if !ok {
		t.Fatalf("second payload = %T, want *packets.LightSetWaveformOptional", controller.sends[1].msg.Payload)
	}
	if payload.SetHue || payload.SetSaturation {
		t.Fatalf("white-only payload set hue/saturation: hue=%v saturation=%v", payload.SetHue, payload.SetSaturation)
	}
	if !payload.SetKelvin || payload.Color.Kelvin != 6500 {
		t.Fatalf("kelvin = %v/%v, want clamped 6500", payload.SetKelvin, payload.Color.Kelvin)
	}
}

func TestLifxTransportSetDeviceStateSendsKelvinColorAsWhite(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       "single",
		On:         true,
		Brightness: 0.5,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 2000, KelvinMax: 9000},
		Color:      &HSLColor{H: 210, S: 0, L: 0.72, Kelvin: 2000},
		Kelvin:     2000,
	}
	transport := NewLifxTransportWithController(controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	payload := controller.sends[1].msg.Payload.(*packets.LightSetWaveformOptional)
	if payload.SetHue {
		t.Fatal("kelvin white command should not set hue")
	}
	if !payload.SetSaturation || payload.Color.Saturation != 0 {
		t.Fatalf("saturation = %v/%v, want set 0", payload.SetSaturation, payload.Color.Saturation)
	}
	if !payload.SetKelvin || payload.Color.Kelvin != 2000 {
		t.Fatalf("kelvin = %v/%v, want set 2000", payload.SetKelvin, payload.Color.Kelvin)
	}
}

func TestLifxTransportSetDeviceStateSendsMultizonePowerAndColors(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       "multizone",
		On:         true,
		Brightness: 0.33,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Kelvin:     5000,
		Zones: []HSLColor{
			{H: 10, S: 0.2, L: 0.4},
			{H: 120, S: 0.8, L: 0.7},
		},
	}
	transport := NewLifxTransportWithController(controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sends) != 2 {
		t.Fatalf("sent %d messages, want 2", len(controller.sends))
	}
	if _, ok := controller.sends[0].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("first payload = %T, want *packets.DeviceSetPower", controller.sends[0].msg.Payload)
	}
	payload, ok := controller.sends[1].msg.Payload.(*packets.MultiZoneExtendedSetColorZones)
	if !ok {
		t.Fatalf("second payload = %T, want *packets.MultiZoneExtendedSetColorZones", controller.sends[1].msg.Payload)
	}
	if payload.Index != 0 || payload.ColorsCount != 2 {
		t.Fatalf("multizone index/count = %d/%d, want 0/2", payload.Index, payload.ColorsCount)
	}
	first := lifxdevice.NewColor(payload.Colors[0])
	if first.Hue != 10 || first.Saturation != 20 || first.Brightness != 40 || first.Kelvin != 5000 {
		t.Fatalf("first zone color = %#v, want h=10 s=20 b=40 k=5000", first)
	}
}

func TestLifxTransportSetDeviceStateSendsDirectMultizoneAsSingleColor(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       "multizone",
		On:         true,
		Brightness: 0.33,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Kelvin:     5000,
		Color:      &HSLColor{H: 10, S: 0.2, L: 0.4},
		Zones: []HSLColor{
			{H: 10, S: 0.2, L: 0.4},
			{H: 120, S: 0.8, L: 0.7},
		},
	}
	transport := NewLifxTransportWithController(controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Preview: true}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sends) != 2 {
		t.Fatalf("sent %d messages, want 2", len(controller.sends))
	}
	if _, ok := controller.sends[1].msg.Payload.(*packets.LightSetWaveformOptional); !ok {
		t.Fatalf("second payload = %T, want *packets.LightSetWaveformOptional", controller.sends[1].msg.Payload)
	}
}

func TestLifxTransportSetDeviceStateSendsMatrixPowerAndColors(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       "matrix",
		On:         true,
		Brightness: 0.66,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Kelvin:     2700,
		Chain: []Matrix{
			{
				ID:   0,
				W:    2,
				Rows: []MatrixRow{{Cols: 2}, {Cols: 2}},
				Pixels: []HSLColor{
					{H: 200, S: 0.5, L: 0.2},
					{H: 210, S: 0.5, L: 0.2},
					{H: 220, S: 0.5, L: 0.2},
					{H: 230, S: 0.5, L: 0.2},
				},
			},
			{
				ID:   1,
				W:    2,
				Rows: []MatrixRow{{Cols: 2}, {Cols: 2}},
				Pixels: []HSLColor{
					{H: 20, S: 0.7, L: 0.3},
					{H: 30, S: 0.7, L: 0.3},
					{H: 40, S: 0.7, L: 0.3},
					{H: 50, S: 0.7, L: 0.3},
				},
			},
		},
	}
	transport := NewLifxTransportWithController(controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sends) != 3 {
		t.Fatalf("sent %d messages, want 3", len(controller.sends))
	}
	firstTile, ok := controller.sends[1].msg.Payload.(*packets.TileSet64)
	if !ok {
		t.Fatalf("second payload = %T, want *packets.TileSet64", controller.sends[1].msg.Payload)
	}
	if firstTile.TileIndex != 0 || firstTile.Length != 2 || firstTile.Rect.Width != 2 {
		t.Fatalf("first tile metadata = index %d length %d width %d, want 0/2/2", firstTile.TileIndex, firstTile.Length, firstTile.Rect.Width)
	}
	firstColor := lifxdevice.NewColor(firstTile.Colors[0])
	if firstColor.Hue != 200 || firstColor.Saturation != 50 || firstColor.Brightness != 20 || firstColor.Kelvin != 2700 {
		t.Fatalf("first matrix color = %#v, want h=200 s=50 b=20 k=2700", firstColor)
	}
	secondTile, ok := controller.sends[2].msg.Payload.(*packets.TileSet64)
	if !ok {
		t.Fatalf("third payload = %T, want *packets.TileSet64", controller.sends[2].msg.Payload)
	}
	if secondTile.TileIndex != 1 {
		t.Fatalf("second tile index = %d, want 1", secondTile.TileIndex)
	}
}

func TestLifxTransportSetDeviceStateRevertsMatrixOrientationWhenSendingPixels(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       "matrix",
		On:         true,
		Brightness: 0.66,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Kelvin:     2700,
		Chain: []Matrix{{
			ID:          0,
			W:           2,
			SendWidth:   2,
			Orientation: int(lifxdevice.OrientationRight),
			Rows:        []MatrixRow{{Cols: 2}, {Cols: 2}},
			Pixels: []HSLColor{
				{H: 20, S: 0.5, L: 0.2},
				{H: 0, S: 0.5, L: 0.2},
				{H: 30, S: 0.5, L: 0.2},
				{H: 10, S: 0.5, L: 0.2},
			},
		}},
	}
	transport := NewLifxTransportWithController(controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sends) != 2 {
		t.Fatalf("sent %d messages, want 2", len(controller.sends))
	}
	payload, ok := controller.sends[1].msg.Payload.(*packets.TileSet64)
	if !ok {
		t.Fatalf("second payload = %T, want *packets.TileSet64", controller.sends[1].msg.Payload)
	}
	assertPayloadHues(t, payload.Colors[:4], []float64{0, 10, 20, 30})
}

func TestLifxTransportSetDeviceStateSendsDirectMatrixAsSingleColor(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       "matrix",
		On:         true,
		Brightness: 0.66,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Kelvin:     2700,
		Color:      &HSLColor{H: 200, S: 0.5, L: 0.2},
		Chain: []Matrix{{
			ID:   0,
			W:    2,
			Rows: []MatrixRow{{Cols: 2}},
			Pixels: []HSLColor{
				{H: 200, S: 0.5, L: 0.2},
				{H: 210, S: 0.5, L: 0.2},
			},
		}},
	}
	transport := NewLifxTransportWithController(controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Preview: true}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sends) != 2 {
		t.Fatalf("sent %d messages, want 2", len(controller.sends))
	}
	if _, ok := controller.sends[1].msg.Payload.(*packets.LightSetWaveformOptional); !ok {
		t.Fatalf("second payload = %T, want *packets.LightSetWaveformOptional", controller.sends[1].msg.Payload)
	}
}

type fakeLifxController struct {
	devices []lifxdevice.Device
	sends   []sentMessage
	closed  bool
}

func (f *fakeLifxController) Close() error {
	f.closed = true
	return nil
}

func (f *fakeLifxController) GetDevices() []lifxdevice.Device {
	return f.devices
}

func (f *fakeLifxController) Send(serial lifxdevice.Serial, msg *protocol.Message) error {
	f.sends = append(f.sends, sentMessage{serial: serial, msg: msg})
	return nil
}

type sentMessage struct {
	serial lifxdevice.Serial
	msg    *protocol.Message
}

func testHSBK(hue float64) packets.LightHsbk {
	return packets.LightHsbk{
		Hue:        lifxdevice.ConvertExternalToDeviceValue(hue, 360),
		Saturation: lifxdevice.ConvertExternalToDeviceValue(50, 100),
		Brightness: lifxdevice.ConvertExternalToDeviceValue(50, 100),
		Kelvin:     3500,
	}
}

func assertPixelHues(t *testing.T, colors []HSLColor, want []float64) {
	t.Helper()
	if len(colors) < len(want) {
		t.Fatalf("colors = %d, want at least %d", len(colors), len(want))
	}
	for i, hue := range want {
		if colors[i].H != hue {
			t.Fatalf("color %d hue = %v, want %v; colors = %#v", i, colors[i].H, hue, colors)
		}
	}
}

func assertPayloadHues(t *testing.T, colors []packets.LightHsbk, want []float64) {
	t.Helper()
	if len(colors) < len(want) {
		t.Fatalf("colors = %d, want at least %d", len(colors), len(want))
	}
	for i, hue := range want {
		got := lifxdevice.NewColor(colors[i])
		if got.Hue != hue {
			t.Fatalf("color %d hue = %v, want %v", i, got.Hue, hue)
		}
	}
}
