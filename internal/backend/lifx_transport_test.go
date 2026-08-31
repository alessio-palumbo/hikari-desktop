package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	lifxclient "github.com/alessio-palumbo/lifxlan-go/pkg/client"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/enums"
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
		LocationID:   lifxdevice.LocationID{0x01},
		Group:        "Desk",
		GroupID:      lifxdevice.GroupID{0x02},
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
	transport := newTestLifxTransport(t, controller)

	snapshot, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if len(snapshot.Locations) != 1 || snapshot.Locations[0].Name != "Studio" {
		t.Fatalf("locations = %#v", snapshot.Locations)
	}
	if snapshot.Locations[0].ID != "lifx-location:01000000-0000-0000-0000-000000000000" {
		t.Fatalf("location ID = %q", snapshot.Locations[0].ID)
	}
	if len(snapshot.Groups) != 1 || snapshot.Groups[0].Name != "Desk" {
		t.Fatalf("groups = %#v", snapshot.Groups)
	}
	if snapshot.Groups[0].ID != "lifx-group:02000000-0000-0000-0000-000000000000" || snapshot.Groups[0].LocationID != snapshot.Locations[0].ID {
		t.Fatalf("group = %#v", snapshot.Groups[0])
	}
	if len(snapshot.Devices) != 1 {
		t.Fatalf("devices = %#v", snapshot.Devices)
	}
	got := snapshot.Devices[0]
	if got.Serial != "d073d501a2c3" || got.Name != "Desk Strip" || got.Kind != "multizone" {
		t.Fatalf("device = %#v", got)
	}
	if got.Brightness != 0.5 {
		t.Fatalf("brightness = %v, want zone summary 0.5", got.Brightness)
	}
	if !got.Capability.HasColor || got.Capability.KelvinMin != 2500 || got.Capability.KelvinMax != 9000 {
		t.Fatalf("capability = %#v, want color with 2500-9000K", got.Capability)
	}
	if len(got.Zones) != 1 || got.Zones[0].L != 0.5 {
		t.Fatalf("zones = %#v", got.Zones)
	}
}

func TestMapLifxDevicesKeepsDuplicateLabelsDistinctByStableID(t *testing.T) {
	first := testLifxDevice(t, "d073d501a2c3", "First", "Home", "Lights")
	first.LocationID = lifxdevice.LocationID{0x01}
	first.GroupID = lifxdevice.GroupID{0x11}
	second := testLifxDevice(t, "d073d501a2c4", "Second", "Home", "Lights")
	second.LocationID = lifxdevice.LocationID{0x02}
	second.GroupID = lifxdevice.GroupID{0x12}

	snapshot := mapLifxDevices([]lifxdevice.Device{first, second})

	if len(snapshot.Locations) != 2 {
		t.Fatalf("locations = %#v", snapshot.Locations)
	}
	if len(snapshot.Groups) != 2 {
		t.Fatalf("groups = %#v", snapshot.Groups)
	}
	if snapshot.Devices[0].GroupID == snapshot.Devices[1].GroupID {
		t.Fatalf("device group IDs collided: %#v", snapshot.Devices)
	}
}

func TestLifxTransportSnapshotReplacesDeviceCache(t *testing.T) {
	first := testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk")
	second := testLifxDevice(t, "d073d501a2c4", "Pendant", "Home", "Kitchen")
	controller := &fakeLifxController{devices: []lifxdevice.Device{first, second}}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if transport.cachedDevice(first.Serial.String()) == nil || transport.cachedDevice(second.Serial.String()) == nil {
		t.Fatal("snapshot did not populate cache")
	}

	controller.setDevices([]lifxdevice.Device{second})
	if _, err := transport.Snapshot(context.Background()); err != nil {
		t.Fatalf("second Snapshot returned error: %v", err)
	}
	if transport.cachedDevice(first.Serial.String()) != nil {
		t.Fatal("stale device remained in cache after replacement snapshot")
	}
	if transport.cachedDevice(second.Serial.String()) == nil {
		t.Fatal("active device missing from cache after replacement snapshot")
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

func TestLifxTransportSnapshotSortsLocationsGroupsAndDevices(t *testing.T) {
	devices := []lifxdevice.Device{
		testLifxDevice(t, "d073d501a2c6", "Zulu Strip", "Studio", "Zebra"),
		testLifxDevice(t, "d073d501a2c5", "Pendant", "Home", "Kitchen"),
		testLifxDevice(t, "d073d501a2c4", "Alpha Strip", "Studio", "Zebra"),
		testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk"),
	}

	snapshot := mapLifxDevices(devices)

	assertNames(t, "locations", locationNames(snapshot.Locations), []string{"Home", "Studio"})
	assertNames(t, "groups", groupNames(snapshot.Groups), []string{"Desk", "Kitchen", "Zebra"})
	assertNames(t, "devices", deviceNames(snapshot.Devices), []string{"Desk Lamp", "Pendant", "Alpha Strip", "Zulu Strip"})
}

func TestLifxTransportSnapshotIncludesSwitchAndMapsHybridAsLight(t *testing.T) {
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
	switchDevice.Relays = []lifxdevice.Relay{{Index: 0, PoweredOn: false}, {Index: 1, PoweredOn: true}}
	switchDevice.ButtonConfigKnown = true
	switchDevice.ButtonConfig = lifxdevice.ButtonConfig{
		HapticDurationMs:  250,
		BacklightOnColor:  lifxdevice.Color{Hue: 210, Saturation: 70, Brightness: 60, Kelvin: 3500},
		BacklightOffColor: lifxdevice.Color{Hue: 40, Saturation: 10, Brightness: 20, Kelvin: 2700},
	}

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
	if len(snapshot.Devices) != 2 {
		t.Fatalf("devices = %#v, want switch and hybrid light", snapshot.Devices)
	}
	bySerial := make(map[string]Device, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		bySerial[device.Serial] = device
	}
	switchMapped := bySerial["d073d501a2c4"]
	if switchMapped.Kind != DeviceKindSwitch {
		t.Fatalf("switch kind = %s, want switch", switchMapped.Kind)
	}
	if !switchMapped.On || len(switchMapped.Relays) != 2 || switchMapped.Relays[1].Index != 1 || !switchMapped.Relays[1].On {
		t.Fatalf("switch relays = %#v on=%v, want relay state mapped", switchMapped.Relays, switchMapped.On)
	}
	if switchMapped.ButtonConfig == nil || !switchMapped.ButtonConfig.Known || switchMapped.ButtonConfig.HapticDurationMS != 250 {
		t.Fatalf("button config = %#v, want known mapped config", switchMapped.ButtonConfig)
	}
	if bySerial["d073d501a2c5"].Kind != DeviceKindMultizone {
		t.Fatalf("hybrid kind = %s, want multizone", bySerial["d073d501a2c5"].Kind)
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

func TestLifxTransportSnapshotSummarizesMultizoneFromZones(t *testing.T) {
	dev := testLifxDevice(t, "d073d501a2c7", "Strip", "Studio", "Desk")
	dev.Color = lifxdevice.Color{Hue: 300, Saturation: 100, Brightness: 100, Kelvin: 9000}
	dev.SetProductInfo(31)
	dev.MultizoneProperties = lifxdevice.MultizoneProperties{
		Zones: []packets.LightHsbk{
			{Brightness: lifxdevice.ConvertExternalToDeviceValue(20, 100), Kelvin: 2700},
			{Brightness: lifxdevice.ConvertExternalToDeviceValue(60, 100), Kelvin: 2700},
		},
	}

	snapshot := mapLifxDevices([]lifxdevice.Device{dev})
	got := snapshot.Devices[0]
	if got.Brightness != 0.4 {
		t.Fatalf("brightness = %v, want zone average 0.4", got.Brightness)
	}
	if got.Color == nil || got.Color.S != 0 || got.Color.Kelvin != 2700 {
		t.Fatalf("color = %#v, want zone white summary", got.Color)
	}
}

func TestLifxTransportSnapshotSummarizesMatrixFromPixels(t *testing.T) {
	serial, err := lifxdevice.SerialFromHex("d073d501a2cb")
	if err != nil {
		t.Fatalf("SerialFromHex returned error: %v", err)
	}
	dev := lifxdevice.Device{
		Serial:    serial,
		Label:     "Matrix",
		Location:  "Studio",
		Group:     "Desk",
		PoweredOn: true,
		Color:     lifxdevice.Color{Hue: 300, Saturation: 100, Brightness: 100, Kelvin: 9000},
		MatrixProperties: lifxdevice.MatrixProperties{
			Width:  2,
			Height: 2,
			NZones: 4,
			ChainZones: [][]packets.LightHsbk{{
				{Brightness: lifxdevice.ConvertExternalToDeviceValue(10, 100), Kelvin: 3500},
				{Brightness: lifxdevice.ConvertExternalToDeviceValue(30, 100), Kelvin: 3500},
				{Brightness: lifxdevice.ConvertExternalToDeviceValue(50, 100), Kelvin: 3500},
				{Brightness: lifxdevice.ConvertExternalToDeviceValue(70, 100), Kelvin: 3500},
			}},
		},
	}
	dev.SetProductInfo(55)

	snapshot := mapLifxDevices([]lifxdevice.Device{dev})
	got := snapshot.Devices[0]
	if got.Brightness != 0.4 {
		t.Fatalf("brightness = %v, want pixel average 0.4", got.Brightness)
	}
	if got.Color == nil || got.Color.S != 0 || got.Color.Kelvin != 3500 {
		t.Fatalf("color = %#v, want pixel white summary", got.Color)
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
	transport := newTestLifxTransport(t, controller)

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
	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: Device{Serial: "d073d501a2c3", Kind: DeviceKindSingle}}); err == nil {
		t.Fatal("SetDeviceState returned nil error, want not started error")
	}
}

func TestLifxTransportStartAutomaticOmitsClientConfig(t *testing.T) {
	controller := &fakeLifxController{}
	var gotConfig *lifxclient.Config
	transport := newLifxTransport(func(cfg *lifxclient.Config) (lifxController, error) {
		gotConfig = cfg
		return controller, nil
	}, func() ([]lifxclient.BroadcastInterface, error) {
		return []lifxclient.BroadcastInterface{testBroadcastInterface("en0", "192.168.1.42", "192.168.1.255")}, nil
	}, &memoryNetworkSettingsStore{})

	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if gotConfig != nil {
		t.Fatalf("config = %#v, want nil automatic config", gotConfig)
	}
}

func TestLifxTransportStartSelectedInterfacePassesClientConfig(t *testing.T) {
	var gotConfig *lifxclient.Config
	store := &memoryNetworkSettingsStore{interfaceName: "en0"}
	transport := newLifxTransport(func(cfg *lifxclient.Config) (lifxController, error) {
		gotConfig = cfg
		return &fakeLifxController{}, nil
	}, func() ([]lifxclient.BroadcastInterface, error) {
		return []lifxclient.BroadcastInterface{testBroadcastInterface("en0", "192.168.1.42", "192.168.1.255")}, nil
	}, store)

	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if gotConfig == nil || gotConfig.BroadcastInterfaceName != "en0" {
		t.Fatalf("config = %#v, want selected interface name", gotConfig)
	}
}

func TestLifxTransportStartMissingSavedInterfaceStaysPinned(t *testing.T) {
	var gotConfig *lifxclient.Config
	store := &memoryNetworkSettingsStore{interfaceName: "en9"}
	transport := newLifxTransport(func(cfg *lifxclient.Config) (lifxController, error) {
		gotConfig = cfg
		return &fakeLifxController{}, nil
	}, func() ([]lifxclient.BroadcastInterface, error) {
		return []lifxclient.BroadcastInterface{testBroadcastInterface("en0", "192.168.1.42", "192.168.1.255")}, nil
	}, store)

	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if gotConfig == nil || gotConfig.BroadcastInterfaceName != "en9" {
		t.Fatalf("config = %#v, want saved interface to remain pinned", gotConfig)
	}
	settings, err := transport.NetworkSettings(context.Background())
	if err != nil {
		t.Fatalf("NetworkSettings returned error: %v", err)
	}
	if settings.SelectedInterfaceName != "en9" || settings.Warning == "" || store.interfaceName != "en9" {
		t.Fatalf("settings = %#v store = %q, want warning with pinned missing interface", settings, store.interfaceName)
	}
}

func TestLifxTransportSetNetworkInterfaceRestartsController(t *testing.T) {
	first := &fakeLifxController{devices: []lifxdevice.Device{testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk")}}
	second := &fakeLifxController{devices: []lifxdevice.Device{testLifxDevice(t, "d073d501a2c4", "Pendant", "Home", "Kitchen")}}
	controllers := []lifxController{first, second}
	configs := []*lifxclient.Config{}
	transport := newLifxTransport(func(cfg *lifxclient.Config) (lifxController, error) {
		configs = append(configs, cfg)
		ctrl := controllers[0]
		controllers = controllers[1:]
		return ctrl, nil
	}, func() ([]lifxclient.BroadcastInterface, error) {
		return []lifxclient.BroadcastInterface{testBroadcastInterface("en0", "192.168.1.42", "192.168.1.255")}, nil
	}, &memoryNetworkSettingsStore{})

	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if _, err := transport.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if transport.cachedDevice("d073d501a2c3") == nil {
		t.Fatal("first snapshot did not populate cache")
	}

	settings, err := transport.SetNetworkInterface(context.Background(), SetNetworkInterfaceRequest{InterfaceName: "en0"})
	if err != nil {
		t.Fatalf("SetNetworkInterface returned error: %v", err)
	}
	if settings.SelectedInterfaceName != "en0" {
		t.Fatalf("settings = %#v, want en0 selected", settings)
	}
	if !first.isClosed() {
		t.Fatal("old controller was not closed")
	}
	if len(configs) != 2 || configs[0] != nil || configs[1] == nil || configs[1].BroadcastInterfaceName != "en0" {
		t.Fatalf("configs = %#v, want automatic then en0", configs)
	}
	if transport.cachedDevice("d073d501a2c3") != nil {
		t.Fatal("old cache was not cleared on restart")
	}
	snapshot, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("second Snapshot returned error: %v", err)
	}
	if len(snapshot.Devices) != 1 || snapshot.Devices[0].Name != "Pendant" {
		t.Fatalf("snapshot = %#v, want new controller devices", snapshot)
	}
}

func TestLifxTransportSnapshotRejectsMissingNetworkInterfaces(t *testing.T) {
	transport := newLifxTransport(func(cfg *lifxclient.Config) (lifxController, error) {
		return &fakeLifxController{devices: []lifxdevice.Device{testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk")}}, nil
	}, func() ([]lifxclient.BroadcastInterface, error) {
		return nil, nil
	}, &memoryNetworkSettingsStore{})

	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if _, err := transport.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot returned nil error, want missing network interface error")
	}
}

func TestLifxTransportSnapshotRejectsMissingSelectedNetworkInterface(t *testing.T) {
	transport := newLifxTransport(func(cfg *lifxclient.Config) (lifxController, error) {
		return &fakeLifxController{devices: []lifxdevice.Device{testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk")}}, nil
	}, func() ([]lifxclient.BroadcastInterface, error) {
		return []lifxclient.BroadcastInterface{testBroadcastInterface("en1", "10.0.0.2", "10.0.0.255")}, nil
	}, &memoryNetworkSettingsStore{interfaceName: "en0"})

	transport.selectedInterfaceName = "en0"
	transport.controller = &fakeLifxController{devices: []lifxdevice.Device{testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk")}}
	if _, err := transport.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot returned nil error, want selected network interface error")
	}
}

func TestLifxTransportSetDeviceStateSendsSingleZoneColor(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Name:       "Test",
		Kind:       DeviceKindSingle,
		On:         true,
		Brightness: 0.42,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 210, S: 0.75, L: 0.6},
		Kelvin:     4000,
	}
	transport := newTestLifxTransport(t, controller)

	got, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device})
	if err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if got.Serial != device.Serial {
		t.Fatalf("SetDeviceState returned %#v, want %#v", got, device)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	if controller.sentMessages()[0].serial.String() != device.Serial {
		t.Fatalf("sent serial = %s, want %s", controller.sentMessages()[0].serial.String(), device.Serial)
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional)
	if !ok {
		t.Fatalf("payload = %T, want *packets.LightSetWaveformOptional", controller.sentMessages()[0].msg.Payload)
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

func TestLifxTransportSetDeviceStateSendsSingleZoneColorBeforePowerOn(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindSingle,
		On:         true,
		Brightness: 0.42,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 210, S: 0.75, L: 0.6},
		Kelvin:     4000,
	}
	transport := newTestLifxTransport(t, controller)
	transport.storeCachedDevice(Device{Serial: device.Serial, On: false})

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Intent: DeviceCommandColor}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 2 {
		t.Fatalf("sent %d messages, want 2", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional); !ok {
		t.Fatalf("first payload = %T, want *packets.LightSetWaveformOptional", controller.sentMessages()[0].msg.Payload)
	}
	if _, ok := controller.sentMessages()[1].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("second payload = %T, want *packets.DeviceSetPower", controller.sentMessages()[1].msg.Payload)
	}
}

func TestLifxTransportSetDeviceStateSendsSingleZonePowerOffOnly(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindSingle, On: false}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Intent: DeviceCommandPower}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.DeviceSetPower)
	if !ok {
		t.Fatalf("payload = %T, want *packets.DeviceSetPower", controller.sentMessages()[0].msg.Payload)
	}
	if payload.Level != 0 {
		t.Fatalf("power level = %d, want 0", payload.Level)
	}
}

func TestLifxTransportSetDeviceStateClampsWhiteOnlyDevice(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindSingle,
		On:         true,
		Brightness: 0.5,
		Capability: DeviceCapability{HasColor: false, KelvinMin: 2700, KelvinMax: 6500},
		Color:      &HSLColor{H: 210, S: 0.75, L: 0.6},
		Kelvin:     9000,
	}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional)
	if !ok {
		t.Fatalf("payload = %T, want *packets.LightSetWaveformOptional", controller.sentMessages()[0].msg.Payload)
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
		Kind:       DeviceKindSingle,
		On:         true,
		Brightness: 0.5,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 2000, KelvinMax: 9000},
		Color:      &HSLColor{H: 210, S: 0, L: 0.72, Kelvin: 2000},
		Kelvin:     2000,
	}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	payload := controller.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional)
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
		Kind:       DeviceKindMultizone,
		On:         true,
		Brightness: 0.33,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Kelvin:     5000,
		Zones: []HSLColor{
			{H: 10, S: 0.2, L: 0.4},
			{H: 120, S: 0.8, L: 0.7},
		},
	}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.MultiZoneExtendedSetColorZones)
	if !ok {
		t.Fatalf("payload = %T, want *packets.MultiZoneExtendedSetColorZones", controller.sentMessages()[0].msg.Payload)
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
		Kind:       DeviceKindMultizone,
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
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Preview: true}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional); !ok {
		t.Fatalf("payload = %T, want *packets.LightSetWaveformOptional", controller.sentMessages()[0].msg.Payload)
	}
}

func TestLifxTransportSetDeviceStateSendsDirectMultizonePowerOnly(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMultizone,
		On:         true,
		Brightness: 0.33,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 10, S: 0.2, L: 0.4},
		Zones:      []HSLColor{{H: 10, S: 0.2, L: 0.4}},
	}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Preview: true, Intent: DeviceCommandPower}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("payload = %T, want *packets.DeviceSetPower", controller.sentMessages()[0].msg.Payload)
	}
}

func TestLifxTransportSetDeviceStateSendsDirectMultizoneBrightnessOnly(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMultizone,
		On:         true,
		Brightness: 0.25,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 120, S: 0.8, L: 0.5},
		Zones: []HSLColor{
			{H: 60, S: 1, L: 0.25},
			{H: 240, S: 1, L: 0.25},
		},
	}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Preview: true, Intent: DeviceCommandBrightness}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional)
	if !ok {
		t.Fatalf("payload = %T, want *packets.LightSetWaveformOptional", controller.sentMessages()[0].msg.Payload)
	}
	assertBrightnessOnlyPayload(t, payload, 25)
}

func TestLifxTransportSetDeviceStateSendsBrightnessBeforePowerOnWhenCachedOff(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMultizone,
		On:         true,
		Brightness: 0.25,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Zones:      []HSLColor{{H: 60, S: 1, L: 0.25}},
	}
	transport := newTestLifxTransport(t, controller)
	transport.storeCachedDevice(Device{Serial: device.Serial, On: false})

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Preview: true, Intent: DeviceCommandBrightness}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 2 {
		t.Fatalf("sent %d messages, want 2", len(controller.sentMessages()))
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional)
	if !ok {
		t.Fatalf("first payload = %T, want *packets.LightSetWaveformOptional", controller.sentMessages()[0].msg.Payload)
	}
	assertBrightnessOnlyPayload(t, payload, 25)
	if _, ok := controller.sentMessages()[1].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("second payload = %T, want *packets.DeviceSetPower", controller.sentMessages()[1].msg.Payload)
	}
}

func TestLifxTransportSetDeviceStateSendsColorBeforePowerOnWhenCachedOff(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindSingle,
		On:         true,
		Brightness: 0.4,
		Color:      &HSLColor{H: 210, S: 0.8, L: 0.4},
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
	}
	transport := newTestLifxTransport(t, controller)
	transport.storeCachedDevice(Device{Serial: device.Serial, On: false})

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Intent: DeviceCommandColor}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 2 {
		t.Fatalf("sent %d messages, want 2", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional); !ok {
		t.Fatalf("first payload = %T, want *packets.LightSetWaveformOptional", controller.sentMessages()[0].msg.Payload)
	}
	if _, ok := controller.sentMessages()[1].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("second payload = %T, want *packets.DeviceSetPower", controller.sentMessages()[1].msg.Payload)
	}
}

func TestLifxTransportSetDeviceStateSendsMatrixPowerAndColors(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
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
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 2 {
		t.Fatalf("sent %d messages, want 2", len(controller.sentMessages()))
	}
	firstTile, ok := controller.sentMessages()[0].msg.Payload.(*packets.TileSet64)
	if !ok {
		t.Fatalf("first payload = %T, want *packets.TileSet64", controller.sentMessages()[0].msg.Payload)
	}
	if firstTile.TileIndex != 0 || firstTile.Length != 2 || firstTile.Rect.Width != 2 {
		t.Fatalf("first tile metadata = index %d length %d width %d, want 0/2/2", firstTile.TileIndex, firstTile.Length, firstTile.Rect.Width)
	}
	firstColor := lifxdevice.NewColor(firstTile.Colors[0])
	if firstColor.Hue != 200 || firstColor.Saturation != 50 || firstColor.Brightness != 20 || firstColor.Kelvin != 2700 {
		t.Fatalf("first matrix color = %#v, want h=200 s=50 b=20 k=2700", firstColor)
	}
	secondTile, ok := controller.sentMessages()[1].msg.Payload.(*packets.TileSet64)
	if !ok {
		t.Fatalf("second payload = %T, want *packets.TileSet64", controller.sentMessages()[1].msg.Payload)
	}
	if secondTile.TileIndex != 1 {
		t.Fatalf("second tile index = %d, want 1", secondTile.TileIndex)
	}
}

func TestLifxTransportSetDeviceStateRevertsMatrixOrientationWhenSendingPixels(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
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
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.TileSet64)
	if !ok {
		t.Fatalf("payload = %T, want *packets.TileSet64", controller.sentMessages()[0].msg.Payload)
	}
	assertPayloadHues(t, payload.Colors[:4], []float64{0, 10, 20, 30})
}

func TestLifxTransportSetDeviceStateSendsDirectMatrixAsSingleColor(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
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
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Preview: true}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional); !ok {
		t.Fatalf("payload = %T, want *packets.LightSetWaveformOptional", controller.sentMessages()[0].msg.Payload)
	}
}

func TestLifxTransportSetDeviceStateSendsDirectMatrixPowerOnly(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
		On:         true,
		Brightness: 0.66,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 200, S: 0.5, L: 0.2},
		Chain: []Matrix{{
			ID:     0,
			W:      2,
			Rows:   []MatrixRow{{Cols: 2}},
			Pixels: []HSLColor{{H: 200, S: 0.5, L: 0.2}, {H: 210, S: 0.5, L: 0.2}},
		}},
	}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Preview: true, Intent: DeviceCommandPower}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("payload = %T, want *packets.DeviceSetPower", controller.sentMessages()[0].msg.Payload)
	}
}

func TestLifxTransportSetDeviceStateSendsDirectMatrixBrightnessOnly(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
		On:         true,
		Brightness: 0.25,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 120, S: 0.8, L: 0.5},
		Chain: []Matrix{{
			ID:   0,
			W:    2,
			Rows: []MatrixRow{{Cols: 2}},
			Pixels: []HSLColor{
				{H: 60, S: 1, L: 0.25},
				{H: 240, S: 1, L: 0.25},
			},
		}},
	}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Preview: true, Intent: DeviceCommandBrightness}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional)
	if !ok {
		t.Fatalf("payload = %T, want *packets.LightSetWaveformOptional", controller.sentMessages()[0].msg.Payload)
	}
	assertBrightnessOnlyPayload(t, payload, 25)
}

func TestLifxTransportSetDeviceStateSendsSwitchRelayPower(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial: "d073d501a2c3",
		Name:   "Wall Switch",
		Kind:   DeviceKindSwitch,
		Relays: []Relay{{Index: 0, On: true}, {Index: 1, On: false}},
	}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Intent: DeviceCommandRelayPower}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 2 {
		t.Fatalf("sent %d messages, want 2", len(controller.sentMessages()))
	}
	first, ok := controller.sentMessages()[0].msg.Payload.(*packets.RelaySetPower)
	if !ok {
		t.Fatalf("first payload = %T, want *packets.RelaySetPower", controller.sentMessages()[0].msg.Payload)
	}
	if first.RelayIndex != 0 || first.Level != math.MaxUint16 {
		t.Fatalf("first relay = %d/%d, want relay 0 on", first.RelayIndex, first.Level)
	}
	second := controller.sentMessages()[1].msg.Payload.(*packets.RelaySetPower)
	if second.RelayIndex != 1 || second.Level != 0 {
		t.Fatalf("second relay = %d/%d, want relay 1 off", second.RelayIndex, second.Level)
	}
}

func TestLifxTransportSetDeviceStateSendsSwitchButtonConfig(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		Serial: "d073d501a2c3",
		Name:   "Wall Switch",
		Kind:   DeviceKindSwitch,
		ButtonConfig: &ButtonConfig{
			Known:             true,
			HapticDurationMS:  250,
			BacklightOnColor:  HSLColor{H: 200, S: 0.75, L: 0.6, Kelvin: 3500},
			BacklightOffColor: HSLColor{H: 40, S: 0.1, L: 0.2, Kelvin: 2700},
		},
	}
	transport := newTestLifxTransport(t, controller)

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device, Intent: DeviceCommandButton}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.ButtonSetConfig)
	if !ok {
		t.Fatalf("payload = %T, want *packets.ButtonSetConfig", controller.sentMessages()[0].msg.Payload)
	}
	if payload.HapticDurationMs != 250 {
		t.Fatalf("haptic = %d, want 250", payload.HapticDurationMs)
	}
	on := lifxdevice.NewColor(packets.LightHsbk(payload.BacklightOnColor))
	if on.Hue != 200 || on.Saturation != 75 || on.Brightness != 60 {
		t.Fatalf("on color = %#v, want h=200 s=75 b=60", on)
	}
}

func TestLifxTransportStartDeviceEffectSendsMultizoneMove(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindMultizone, On: true}

	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectMove, SpeedMS: 1200, Direction: "reverse"})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	if status.Serial != device.Serial || !status.Running || status.Effect != string(DeviceEffectMove) {
		t.Fatalf("status = %#v, want running move status", status)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.MultiZoneSetEffect)
	if !ok {
		t.Fatalf("payload = %T, want *packets.MultiZoneSetEffect", controller.sentMessages()[0].msg.Payload)
	}
	if payload.Settings.Type != enums.MultiZoneEffectTypeMULTIZONEEFFECTTYPEMOVE {
		t.Fatalf("effect type = %v, want move", payload.Settings.Type)
	}
	if payload.Settings.Speed != 1200 {
		t.Fatalf("speed = %d, want 1200", payload.Settings.Speed)
	}
	if payload.Settings.Parameter.Parameter1 != 0 {
		t.Fatalf("direction parameter = %d, want reverse", payload.Settings.Parameter.Parameter1)
	}
}

func TestLifxTransportStartDeviceEffectDefaultsMultizoneMove(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindMultizone, On: true}

	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	payload := controller.sentMessages()[0].msg.Payload.(*packets.MultiZoneSetEffect)
	if status.Effect != string(DeviceEffectMove) {
		t.Fatalf("effect = %q, want move", status.Effect)
	}
	if payload.Settings.Speed != uint32(defaultFirmwareEffectSpeed.Milliseconds()) {
		t.Fatalf("speed = %d, want default", payload.Settings.Speed)
	}
	if payload.Settings.Parameter.Parameter1 != 1 {
		t.Fatalf("direction parameter = %d, want forward", payload.Settings.Parameter.Parameter1)
	}
}

func TestLifxTransportStartDeviceEffectRunsMultizoneAppEffects(t *testing.T) {
	effects := []DeviceEffect{DeviceEffectFlow, DeviceEffectComet, DeviceEffectSparkle, DeviceEffectScanner}
	for _, effect := range effects {
		t.Run(string(effect), func(t *testing.T) {
			lifx := testLifxDevice(t, "d073d501a2c3", "Strip", "Home", "Desk")
			lifx.SetProductInfo(31)
			lifx.MultizoneProperties.Zones = []packets.LightHsbk{testHSBK(20), testHSBK(120), testHSBK(220)}
			controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}, sent: make(chan sentMessage, 4)}
			transport := newTestLifxTransport(t, controller)
			device := mapLifxDevice(lifx, "desk")

			status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: effect, SpeedMS: 2000})
			if err != nil {
				t.Fatalf("StartDeviceEffect returned error: %v", err)
			}
			if status.Effect != string(effect) || !status.Running {
				t.Fatalf("status = %#v, want running %s", status, effect)
			}
			payload := waitForMultizoneSetColors(t, controller.sent)
			if payload.Index != 0 || payload.ColorsCount != 3 {
				t.Fatalf("multizone index/count = %d/%d, want 0/3", payload.Index, payload.ColorsCount)
			}
			transport.stopAllAppEffects()
		})
	}
}

func TestLifxTransportStartDeviceEffectUsesCachedAppliedState(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Strip", "Home", "Desk")
	lifx.SetProductInfo(31)
	lifx.MultizoneProperties.Zones = []packets.LightHsbk{testHSBK(20), testHSBK(20), testHSBK(20)}
	controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}, sent: make(chan sentMessage, 8)}
	transport := newTestLifxTransport(t, controller)

	stale := mapLifxDevice(lifx, "desk")
	applied := stale
	applied.Color = &HSLColor{H: 120, S: 0.5, L: 0.5}
	applied.Zones = []HSLColor{{H: 120, S: 0.5, L: 0.5}, {H: 120, S: 0.5, L: 0.5}, {H: 120, S: 0.5, L: 0.5}}
	transport.storeCachedDevice(applied)

	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: stale, Effect: DeviceEffectComet, SpeedMS: 2000}); err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	waitForMultizoneSetColors(t, controller.sent)
	controller.resetSends()

	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: stale}); err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}

	sends := controller.sentMessages()
	var restored *packets.MultiZoneExtendedSetColorZones
	for index := range sends {
		if payload, ok := sends[index].msg.Payload.(*packets.MultiZoneExtendedSetColorZones); ok {
			restored = payload
		}
	}
	if restored == nil {
		t.Fatalf("stop sends = %d, want multizone restore payload", len(sends))
	}
	color := lifxdevice.NewColor(restored.Colors[0])
	if color.Hue != 120 {
		t.Fatalf("restored hue = %v, want cached applied hue 120", color.Hue)
	}
}

func TestLifxTransportSetDeviceStateWhileAppEffectRunningUpdatesRestoreColor(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Strip", "Home", "Desk")
	lifx.SetProductInfo(31)
	lifx.MultizoneProperties.Zones = []packets.LightHsbk{testHSBK(20), testHSBK(20), testHSBK(20)}
	controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}, sent: make(chan sentMessage, 16)}
	transport := newTestLifxTransport(t, controller)
	device := mapLifxDevice(lifx, "desk")
	transport.storeCachedDevice(device)

	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectComet, SpeedMS: 2000}); err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	waitForMultizoneSetColors(t, controller.sent)

	changed := device
	changed.Color = &HSLColor{H: 220, S: 0.8, L: 0.5}
	changed.Zones = []HSLColor{{H: 220, S: 0.8, L: 0.5}, {H: 220, S: 0.8, L: 0.5}, {H: 220, S: 0.8, L: 0.5}}
	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: changed, Preview: true, Intent: DeviceCommandColor}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	waitForMultizoneSetColors(t, controller.sent)

	controller.resetSends()
	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: changed}); err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}

	sends := controller.sentMessages()
	var restored *packets.MultiZoneExtendedSetColorZones
	for index := range sends {
		if payload, ok := sends[index].msg.Payload.(*packets.MultiZoneExtendedSetColorZones); ok {
			restored = payload
		}
	}
	if restored == nil {
		t.Fatalf("stop sends = %d, want multizone restore payload", len(sends))
	}
	color := lifxdevice.NewColor(restored.Colors[0])
	if color.Hue != 220 {
		t.Fatalf("restored hue = %v, want changed hue 220", color.Hue)
	}
}

func TestLifxTransportSetDeviceStateWhileAppEffectRunningUpdatesRestoreBrightness(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Strip", "Home", "Desk")
	lifx.SetProductInfo(31)
	lifx.MultizoneProperties.Zones = []packets.LightHsbk{testHSBK(20), testHSBK(20), testHSBK(20)}
	controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}, sent: make(chan sentMessage, 16)}
	transport := newTestLifxTransport(t, controller)
	device := mapLifxDevice(lifx, "desk")
	transport.storeCachedDevice(device)

	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectComet, SpeedMS: 2000}); err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	waitForMultizoneSetColors(t, controller.sent)

	changed := device
	changed.Brightness = 0.8
	changed.Color = &HSLColor{H: 20, S: 0.5, L: 0.8}
	changed.Zones = []HSLColor{{H: 20, S: 0.5, L: 0.8}, {H: 20, S: 0.5, L: 0.8}, {H: 20, S: 0.5, L: 0.8}}
	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: changed, Preview: true, Intent: DeviceCommandBrightness}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	waitForMultizoneSetColors(t, controller.sent)

	controller.resetSends()
	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: changed}); err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}

	sends := controller.sentMessages()
	var restored *packets.MultiZoneExtendedSetColorZones
	for index := range sends {
		if payload, ok := sends[index].msg.Payload.(*packets.MultiZoneExtendedSetColorZones); ok {
			restored = payload
		}
	}
	if restored == nil {
		t.Fatalf("stop sends = %d, want multizone restore payload", len(sends))
	}
	color := lifxdevice.NewColor(restored.Colors[0])
	if math.Abs(color.Brightness-80) > 0.1 {
		t.Fatalf("restored brightness = %v, want 80", color.Brightness)
	}
}

func TestAppEffectCometPaletteKeepsSingleColorBackgroundAndContrastingHead(t *testing.T) {
	device := Device{
		Kind:       DeviceKindMultizone,
		On:         true,
		Brightness: 0.5,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 120, S: 0.5, L: 0.5, Kelvin: 3500},
		Zones:      []HSLColor{{H: 120, S: 0.5, L: 0.5, Kelvin: 3500}, {H: 120, S: 0.5, L: 0.5, Kelvin: 3500}},
		Kelvin:     3500,
	}

	palette := appEffectCometPalette(device)
	if len(palette.Backgrounds) != 1 {
		t.Fatalf("backgrounds = %d, want 1", len(palette.Backgrounds))
	}
	if palette.Backgrounds[0].Hue != 120 {
		t.Fatalf("background hue = %v, want original hue 120", palette.Backgrounds[0].Hue)
	}
	if len(palette.Base) != 1 || palette.Base[0].Hue == palette.Backgrounds[0].Hue {
		t.Fatalf("base palette = %#v, want derived tail color away from background", palette.Base)
	}
	if len(palette.Accents) != 1 || palette.Accents[0].Hue == palette.Backgrounds[0].Hue {
		t.Fatalf("accents = %#v, want contrasting comet head", palette.Accents)
	}
	if hueDistance(palette.Accents[0].Hue, 32) > 70 {
		t.Fatalf("accent hue = %v, want warm comet head", palette.Accents[0].Hue)
	}
	if hueDistance(palette.Base[0].Hue, palette.Accents[0].Hue) < 90 {
		t.Fatalf("tail hue = %v, head hue = %v, want cool tail away from warm head", palette.Base[0].Hue, palette.Accents[0].Hue)
	}
	if palette.Accents[0].Brightness <= palette.Backgrounds[0].Brightness {
		t.Fatalf("accent brightness = %v, background = %v, want brighter head", palette.Accents[0].Brightness, palette.Backgrounds[0].Brightness)
	}
}

func TestSamplePaletteColorsHandlesSingleStop(t *testing.T) {
	colors := []HSLColor{{H: 20, S: 0.5, L: 0.5}, {H: 120, S: 0.5, L: 0.5}, {H: 220, S: 0.5, L: 0.5}}

	sampled := samplePaletteColors(colors, 1)
	if len(sampled) != 1 {
		t.Fatalf("sampled = %d colors, want 1", len(sampled))
	}
	if sampled[0].H != 20 {
		t.Fatalf("sampled hue = %v, want first color hue 20", sampled[0].H)
	}
}

func TestAppEffectBackgroundColorPicksFromMultipleZoneHues(t *testing.T) {
	device := Device{
		Kind:       DeviceKindMultizone,
		On:         true,
		Brightness: 0.5,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 20, S: 0.5, L: 0.5, Kelvin: 3500},
		Zones: []HSLColor{
			{H: 20, S: 0.5, L: 0.5, Kelvin: 3500},
			{H: 120, S: 0.5, L: 0.5, Kelvin: 3500},
			{H: 220, S: 0.5, L: 0.5, Kelvin: 3500},
		},
		Kelvin: 3500,
	}

	background := appEffectBackgroundColor(device, lifxeffects.Color{Hue: 300})
	if background.Hue == 300 {
		t.Fatal("background fell back to the caller default, want a zone color")
	}
}

func TestAppEffectMotionPaletteDerivesVisibleSingleColorVariation(t *testing.T) {
	device := Device{
		Kind:       DeviceKindMultizone,
		On:         true,
		Brightness: 0.5,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 120, S: 0.35, L: 0.5, Kelvin: 3500},
		Zones:      []HSLColor{{H: 120, S: 0.35, L: 0.5, Kelvin: 3500}, {H: 120, S: 0.35, L: 0.5, Kelvin: 3500}},
		Kelvin:     3500,
	}

	palette := appEffectMotionPalette(device)
	if len(palette.Backgrounds) != 1 {
		t.Fatalf("backgrounds = %d, want 1", len(palette.Backgrounds))
	}
	if palette.Backgrounds[0].Hue != 120 {
		t.Fatalf("background hue = %v, want original hue 120", palette.Backgrounds[0].Hue)
	}
	if len(palette.Accents) == 0 {
		t.Fatal("accents is empty")
	}
	if hueDistance(palette.Backgrounds[0].Hue, palette.Accent().Hue) < 45 {
		t.Fatalf("accent hue = %v, background hue = %v, want visible variation", palette.Accent().Hue, palette.Backgrounds[0].Hue)
	}
	if palette.Accent().Saturation < 70 {
		t.Fatalf("accent saturation = %v, want boosted single-color variation", palette.Accent().Saturation)
	}
}

func TestAppEffectScannerPaletteBuildsHighContrastSingleColorAccent(t *testing.T) {
	device := Device{
		Kind:       DeviceKindMultizone,
		On:         true,
		Brightness: 0.45,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 210, S: 0.6, L: 0.45, Kelvin: 3500},
		Zones:      []HSLColor{{H: 210, S: 0.6, L: 0.45, Kelvin: 3500}, {H: 210, S: 0.6, L: 0.45, Kelvin: 3500}},
		Kelvin:     3500,
	}

	palette := appEffectScannerPalette(device)
	if len(palette.Backgrounds) != 1 || len(palette.Accents) != 1 {
		t.Fatalf("palette = %#v, want one background and one accent", palette)
	}
	background := palette.Backgrounds[0]
	accent := palette.Accents[0]
	if hueDistance(background.Hue, accent.Hue) < 120 {
		t.Fatalf("accent hue = %v, background hue = %v, want high contrast", accent.Hue, background.Hue)
	}
	if accent.Brightness <= background.Brightness+20 {
		t.Fatalf("accent brightness = %v, background = %v, want visible scanner head", accent.Brightness, background.Brightness)
	}
	if background.Brightness >= 45 {
		t.Fatalf("background brightness = %v, want slightly reduced background", background.Brightness)
	}
}

func TestAppEffectSparklePaletteAddsVariedAccentsForSingleColor(t *testing.T) {
	device := Device{
		Kind:       DeviceKindMatrix,
		On:         true,
		Brightness: 0.4,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 30, S: 0.5, L: 0.4, Kelvin: 3500},
		Chain: []Matrix{{
			Pixels: []HSLColor{{H: 30, S: 0.5, L: 0.4, Kelvin: 3500}, {H: 30, S: 0.5, L: 0.4, Kelvin: 3500}},
		}},
		Kelvin: 3500,
	}

	palette := appEffectSparklePalette(device)
	if len(palette.Backgrounds) != 1 || len(palette.Accents) == 0 {
		t.Fatalf("palette = %#v, want background and accents", palette)
	}
	for _, accent := range palette.Accents {
		if hueDistance(palette.Backgrounds[0].Hue, accent.Hue) < 45 {
			t.Fatalf("accent hue = %v, background hue = %v, want visible sparkle variation", accent.Hue, palette.Backgrounds[0].Hue)
		}
		if accent.Brightness <= palette.Backgrounds[0].Brightness {
			t.Fatalf("accent brightness = %v, background = %v, want brighter sparkle", accent.Brightness, palette.Backgrounds[0].Brightness)
		}
	}
}

func TestLifxTransportStartDeviceEffectSendsMatrixFlame(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindMatrix, On: true}

	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectFlame, SpeedMS: 900})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	if status.Serial != device.Serial || !status.Running || status.Effect != string(DeviceEffectFlame) {
		t.Fatalf("status = %#v, want running flame status", status)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect)
	if !ok {
		t.Fatalf("payload = %T, want *packets.TileSetEffect", controller.sentMessages()[0].msg.Payload)
	}
	if payload.Settings.Type != enums.TileEffectTypeTILEEFFECTTYPEFLAME {
		t.Fatalf("effect type = %v, want flame", payload.Settings.Type)
	}
	if payload.Settings.Speed != 900 {
		t.Fatalf("speed = %d, want 900", payload.Settings.Speed)
	}
}

func TestLifxTransportStartFirmwareEffectPowersOnOffMatrix(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindMatrix, On: false}

	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectFlame, SpeedMS: 900})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	if status.Effect != string(DeviceEffectFlame) || !status.Running {
		t.Fatalf("status = %#v, want running flame", status)
	}
	if len(controller.sentMessages()) != 2 {
		t.Fatalf("sent %d messages, want effect and power-on", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect); !ok {
		t.Fatalf("first payload = %T, want *packets.TileSetEffect", controller.sentMessages()[0].msg.Payload)
	}
	if _, ok := controller.sentMessages()[1].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("second payload = %T, want *packets.DeviceSetPower", controller.sentMessages()[1].msg.Payload)
	}
}

func TestLifxTransportStartDeviceEffectSendsMatrixMorph(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
		On:         true,
		Firmware:   "4.8",
		Brightness: 0.5,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Chain: []Matrix{{
			Pixels: []HSLColor{{H: 10, S: 0.8, L: 0.4}, {H: 220, S: 0.7, L: 0.6}},
		}},
	}

	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectMorph, SpeedMS: 1400})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	if status.Effect != string(DeviceEffectMorph) || !status.Running {
		t.Fatalf("status = %#v, want running morph", status)
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect)
	if !ok {
		t.Fatalf("payload = %T, want *packets.TileSetEffect", controller.sentMessages()[0].msg.Payload)
	}
	if payload.Settings.Type != enums.TileEffectTypeTILEEFFECTTYPEMORPH {
		t.Fatalf("effect type = %v, want morph", payload.Settings.Type)
	}
	if payload.Settings.PaletteCount != 2 {
		t.Fatalf("palette count = %d, want 2", payload.Settings.PaletteCount)
	}
	if payload.Settings.Speed != 1400 {
		t.Fatalf("speed = %d, want 1400", payload.Settings.Speed)
	}
}

func TestLifxTransportStartDeviceEffectMorphUsesVisibleMatrixPalette(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
		On:         true,
		Brightness: 0.5,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 120, S: 1, L: 0.5},
		Chain: []Matrix{{
			Rows: []MatrixRow{{Cols: 4, HiddenCols: []int{0, 3}}},
			Pixels: []HSLColor{
				{H: 0, S: 0, L: 0},
				{H: 40, S: 1, L: 0.4},
				{H: 80, S: 1, L: 0.6},
				{H: 160, S: 1, L: 0.5},
			},
		}},
	}

	_, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectMorph})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	payload := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect)
	if payload.Settings.PaletteCount != 2 {
		t.Fatalf("palette count = %d, want 2", payload.Settings.PaletteCount)
	}
	assertPayloadHues(t, payload.Settings.Palette[:int(payload.Settings.PaletteCount)], []float64{40, 80})
}

func TestLifxTransportStartDeviceEffectMorphUsesRepresentativeMatrixPalette(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	pixels := make([]HSLColor, 32)
	for i := range pixels {
		pixels[i] = HSLColor{H: float64(i) * 360 / float64(len(pixels)), S: 1, L: 0.2}
	}
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
		On:         true,
		Brightness: 0.7,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Chain: []Matrix{{
			Rows:   []MatrixRow{{Cols: 16}, {Cols: 16}},
			Pixels: pixels,
		}},
	}

	_, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectMorph})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	payload := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect)
	if payload.Settings.PaletteCount != matrixEffectPaletteMaxColors {
		t.Fatalf("palette count = %d, want %d", payload.Settings.PaletteCount, matrixEffectPaletteMaxColors)
	}
	palette := payload.Settings.Palette[:int(payload.Settings.PaletteCount)]
	assertPayloadHueClose(t, palette[0], 5.62)
	assertPayloadHueClose(t, palette[len(palette)-1], 343.12)
	assertPayloadBrightness(t, palette, 100)
}

func TestLifxTransportStartDeviceEffectMorphFallsBackWhenVisibleMatrixIsDark(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
		On:         true,
		Brightness: 0.5,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 120, S: 1, L: 0.5},
		Chain: []Matrix{{
			Rows:   []MatrixRow{{Cols: 2}},
			Pixels: []HSLColor{{H: 10, S: 1, L: 0}, {H: 220, S: 1, L: 0}},
		}},
	}

	_, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectMorph})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	payload := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect)
	if payload.Settings.PaletteCount != 3 {
		t.Fatalf("palette count = %d, want fallback palette", payload.Settings.PaletteCount)
	}
	assertPayloadHues(t, payload.Settings.Palette[:int(payload.Settings.PaletteCount)], []float64{28, 200, 280})
}

func TestLifxTransportStartDeviceEffectMorphUsesFullPaletteBrightness(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
		On:         true,
		Brightness: 0.07,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 240, S: 1, L: 0.07},
		Chain: []Matrix{{
			Rows:   []MatrixRow{{Cols: 2}},
			Pixels: []HSLColor{{H: 240, S: 1, L: 0.07}, {H: 280, S: 1, L: 0.07}},
		}},
	}

	_, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectMorph})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	payload := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect)
	assertPayloadBrightness(t, payload.Settings.Palette[:int(payload.Settings.PaletteCount)], 100)
}

func TestAppEffectPaletteDerivesVariationFromSingleColor(t *testing.T) {
	device := Device{
		Kind:       DeviceKindMultizone,
		Brightness: 0.6,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Zones: []HSLColor{
			{H: 210, S: 0.8, L: 0.6, Kelvin: 3500},
			{H: 210, S: 0.8, L: 0.6, Kelvin: 3500},
			{H: 210, S: 0.8, L: 0.6, Kelvin: 3500},
		},
	}

	palette := appEffectPalette(device)

	if len(palette) < 3 {
		t.Fatalf("palette = %#v, want derived color variation", palette)
	}
	if palette[0].Hue == palette[1].Hue || palette[1].Hue == palette[2].Hue {
		t.Fatalf("palette hues = %#v, want varied hues", palette)
	}
}

func TestAppEffectPaletteDerivesKelvinVariationFromSingleWhite(t *testing.T) {
	device := Device{
		Kind:       DeviceKindMultizone,
		Brightness: 0.6,
		Kelvin:     3500,
		Capability: DeviceCapability{HasColor: false, KelvinMin: 2500, KelvinMax: 6500},
		Zones: []HSLColor{
			{S: 0, L: 0.6, Kelvin: 3500},
			{S: 0, L: 0.6, Kelvin: 3500},
		},
	}

	palette := appEffectPalette(device)

	if len(palette) < 3 {
		t.Fatalf("palette = %#v, want derived kelvin variation", palette)
	}
	if palette[0].Saturation != 0 || palette[1].Saturation != 0 || palette[2].Saturation != 0 {
		t.Fatalf("palette = %#v, want white palette", palette)
	}
	if palette[0].Kelvin >= palette[1].Kelvin || palette[1].Kelvin >= palette[2].Kelvin {
		t.Fatalf("palette kelvin = %#v, want increasing kelvin variation", palette)
	}
}

func TestLifxTransportStartDeviceEffectMorphOnlySendsEffect(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{
		Serial:     "d073d501a2c3",
		Kind:       DeviceKindMatrix,
		On:         true,
		Brightness: 0.07,
		Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		Color:      &HSLColor{H: 240, S: 1, L: 0.07},
		Chain: []Matrix{{
			Rows:   []MatrixRow{{Cols: 2}},
			Pixels: []HSLColor{{H: 240, S: 1, L: 0.07}, {H: 280, S: 1, L: 0.07}},
		}},
	}

	_, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectMorph})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want only morph effect", len(controller.sentMessages()))
	}
	payload := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect)
	assertPayloadBrightness(t, payload.Settings.Palette[:int(payload.Settings.PaletteCount)], 100)
}

func TestLifxTransportStartDeviceEffectAllowsMorphBeforeFirmware48(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindMatrix, On: true, Firmware: "4.7"}

	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectMorph})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	if status.Effect != string(DeviceEffectMorph) || !status.Running {
		t.Fatalf("status = %#v, want running morph", status)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
}

func TestLifxTransportStartDeviceEffectSendsMatrixClouds(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindMatrix, On: true, Firmware: "4.9"}

	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectClouds, SpeedMS: 2200})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	if status.Effect != string(DeviceEffectClouds) || !status.Running {
		t.Fatalf("status = %#v, want running clouds", status)
	}
	payload, ok := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect)
	if !ok {
		t.Fatalf("payload = %T, want *packets.TileSetEffect", controller.sentMessages()[0].msg.Payload)
	}
	if payload.Settings.Type != enums.TileEffectTypeTILEEFFECTTYPESKY {
		t.Fatalf("effect type = %v, want sky", payload.Settings.Type)
	}
	if payload.Settings.Parameter.Parameter0 != uint32(enums.TileEffectSkyTypeTILEEFFECTSKYTYPECLOUDS) {
		t.Fatalf("sky effect = %d, want clouds", payload.Settings.Parameter.Parameter0)
	}
	if payload.Settings.Speed != 2200 {
		t.Fatalf("speed = %d, want 2200", payload.Settings.Speed)
	}
}

func TestLifxTransportStartDeviceEffectRejectsUnsupportedMatrixFirmware(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindMatrix, Firmware: "4.7"}

	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectClouds})
	if err == nil {
		t.Fatal("StartDeviceEffect returned nil error, want firmware error")
	}
	if status.Running || status.Error == "" {
		t.Fatalf("status = %#v, want stopped error status", status)
	}
	if len(controller.sentMessages()) != 0 {
		t.Fatalf("sent %d messages, want 0", len(controller.sentMessages()))
	}
}

func TestLifxTransportStartDeviceEffectRejectsUnsupportedDevice(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindSingle}

	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectFlame})
	if err == nil {
		t.Fatal("StartDeviceEffect returned nil error, want unsupported error")
	}
	if status.Running || status.Error == "" {
		t.Fatalf("status = %#v, want stopped error status", status)
	}
	if len(controller.sentMessages()) != 0 {
		t.Fatalf("sent %d messages, want 0", len(controller.sentMessages()))
	}
}

func TestLifxTransportStartDeviceEffectRejectsMatrixStripEffects(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindMatrix}

	for _, effect := range []DeviceEffect{DeviceEffectComet} {
		t.Run(string(effect), func(t *testing.T) {
			status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: effect})
			if err == nil {
				t.Fatal("StartDeviceEffect returned nil error, want unsupported effect error")
			}
			if status.Running || status.Error == "" {
				t.Fatalf("status = %#v, want stopped error status", status)
			}
			if len(controller.sentMessages()) != 0 {
				t.Fatalf("sent %d messages, want 0", len(controller.sentMessages()))
			}
		})
	}
}

func TestLifxTransportStartDeviceEffectRunsSnakeAcrossMatrixChain(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Tiles", "Home", "Desk")
	lifx.SetProductInfo(55)
	lifx.MatrixProperties.Width = 8
	lifx.MatrixProperties.Height = 8
	lifx.MatrixProperties.NZones = 64
	lifx.MatrixProperties.ChainLength = 2
	lifx.MatrixProperties.ChainZones = [][]packets.LightHsbk{
		make([]packets.LightHsbk, 64),
		make([]packets.LightHsbk, 64),
	}
	controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}, sent: make(chan sentMessage, 4)}
	transport := newTestLifxTransport(t, controller)
	device := mapLifxDevice(lifx, "desk")

	status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectSnake, SpeedMS: 12000})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	if status.Effect != string(DeviceEffectSnake) || !status.Running {
		t.Fatalf("status = %#v, want running snake", status)
	}
	firstPayload := waitForTileSet64(t, controller.sent)
	sends := controller.sentMessages()
	if len(sends) == 0 {
		t.Fatal("sent no messages, want app effect frame")
	}
	if _, ok := sends[0].msg.Payload.(*packets.TileSet64); !ok {
		t.Fatalf("first app effect payload = %T, want *packets.TileSet64", sends[0].msg.Payload)
	}
	if firstPayload.TileIndex != 0 || firstPayload.Length != 1 {
		t.Fatalf("tile metadata = index %d length %d, want 0/1", firstPayload.TileIndex, firstPayload.Length)
	}
	secondPayload := waitForTileSet64(t, controller.sent)
	if secondPayload.TileIndex != 1 || secondPayload.Length != 1 {
		t.Fatalf("second tile metadata = index %d length %d, want 1/1", secondPayload.TileIndex, secondPayload.Length)
	}
}

func TestLifxTransportSnakeSpeedControlsRunnerStep(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Tiles", "Home", "Desk")
	lifx.SetProductInfo(55)
	lifx.MatrixProperties.Width = 8
	lifx.MatrixProperties.Height = 8
	lifx.MatrixProperties.NZones = 64
	lifx.MatrixProperties.ChainLength = 1

	got := appEffectStep(StartDeviceEffectRequest{Device: mapLifxDevice(lifx, "desk"), Effect: DeviceEffectSnake, SpeedMS: 12000}, lifx)
	want := 12000 * time.Millisecond / 69
	if got != want {
		t.Fatalf("snake step = %s, want %s", got, want)
	}
}

func TestLifxTransportScannerDefaultPeriodDependsOnDeviceKind(t *testing.T) {
	if got := appEffectScannerPeriod(DeviceKindMultizone); got != 4*time.Second {
		t.Fatalf("multizone scanner period = %s, want 4s", got)
	}
	if got := appEffectScannerPeriod(DeviceKindMatrix); got != 2*time.Second {
		t.Fatalf("matrix scanner period = %s, want 2s", got)
	}
}

func TestLifxTransportStartDeviceEffectPowersOnOffMatrix(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Tiles", "Home", "Desk")
	lifx.PoweredOn = false
	lifx.SetProductInfo(55)
	lifx.MatrixProperties.Width = 8
	lifx.MatrixProperties.Height = 8
	lifx.MatrixProperties.NZones = 64
	lifx.MatrixProperties.ChainLength = 1
	lifx.MatrixProperties.ChainZones = [][]packets.LightHsbk{make([]packets.LightHsbk, 64)}
	controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}, sent: make(chan sentMessage, 4)}
	transport := newTestLifxTransport(t, controller)
	device := mapLifxDevice(lifx, "desk")

	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectSnake}); err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	sends := controller.sentMessages()
	if len(sends) < 2 {
		t.Fatalf("sent %d messages, want effect frame before power-on", len(sends))
	}
	if _, ok := sends[0].msg.Payload.(*packets.TileSet64); !ok {
		t.Fatalf("first payload = %T, want *packets.TileSet64", sends[0].msg.Payload)
	}
	if _, ok := sends[1].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("second payload = %T, want *packets.DeviceSetPower", sends[1].msg.Payload)
	}
	waitForTileSet64(t, controller.sent)
}

func TestLifxTransportStartDeviceEffectRunsHikariMatrixEffects(t *testing.T) {
	effects := []DeviceEffect{
		DeviceEffectWorm,
		DeviceEffectFrames,
		DeviceEffectWaterfall,
		DeviceEffectRockets,
		DeviceEffectWave,
		DeviceEffectRing,
		DeviceEffectFlow,
		DeviceEffectSparkle,
		DeviceEffectScanner,
	}
	for _, effect := range effects {
		t.Run(string(effect), func(t *testing.T) {
			lifx := testLifxDevice(t, "d073d501a2c3", "Tiles", "Home", "Desk")
			lifx.SetProductInfo(55)
			lifx.MatrixProperties.Width = 8
			lifx.MatrixProperties.Height = 8
			lifx.MatrixProperties.NZones = 64
			lifx.MatrixProperties.ChainLength = 1
			lifx.MatrixProperties.ChainZones = [][]packets.LightHsbk{{testHSBK(20)}}
			controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}, sent: make(chan sentMessage, 4)}
			transport := newTestLifxTransport(t, controller)
			device := mapLifxDevice(lifx, "desk")

			status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: effect, SpeedMS: 10000})
			if err != nil {
				t.Fatalf("StartDeviceEffect returned error: %v", err)
			}
			if status.Effect != string(effect) || !status.Running {
				t.Fatalf("status = %#v, want running %s", status, effect)
			}
			payload := waitForTileSet64(t, controller.sent)
			if payload.TileIndex != 0 || payload.Length != 1 {
				t.Fatalf("tile metadata = index %d length %d, want 0/1", payload.TileIndex, payload.Length)
			}
			transport.stopAllAppEffects()
		})
	}
}

func TestLifxTransportStopDeviceEffectRestoresCachedMatrixAfterSnake(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Tiles", "Home", "Desk")
	lifx.SetProductInfo(55)
	lifx.MatrixProperties.Width = 2
	lifx.MatrixProperties.Height = 1
	lifx.MatrixProperties.NZones = 2
	lifx.MatrixProperties.ChainLength = 1
	lifx.MatrixProperties.ChainZones = [][]packets.LightHsbk{{testHSBK(20), testHSBK(120)}}
	controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}, sent: make(chan sentMessage, 4)}
	transport := newTestLifxTransport(t, controller)
	device := mapLifxDevice(lifx, "desk")
	device.Chain[0].Pixels[0] = HSLColor{H: 20, S: 0.5, L: 0.5}
	device.Chain[0].Pixels[1] = HSLColor{H: 120, S: 0.5, L: 0.5}
	transport.storeCachedDevice(device)

	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectSnake}); err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	waitForTileSet64(t, controller.sent)
	controller.resetSends()

	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: device}); err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}
	sends := controller.sentMessages()
	if len(sends) == 0 {
		t.Fatal("stop did not send restore messages")
	}
	for _, sent := range sends {
		if _, ok := sent.msg.Payload.(*packets.TileSetEffect); ok {
			t.Fatalf("stop sent firmware effect-off during app restore: %#v", sent.msg.Payload)
		}
	}
	payload, ok := sends[len(sends)-1].msg.Payload.(*packets.TileSet64)
	if !ok {
		t.Fatalf("last payload = %T, want *packets.TileSet64", sends[len(sends)-1].msg.Payload)
	}
	restored := lifxdevice.NewColor(payload.Colors[0])
	if restored.Hue != 20 {
		t.Fatalf("restored hue = %v, want cached hue 20", restored.Hue)
	}
}

func TestLifxTransportStopFirmwareEffectDoesNotRestoreMatrixState(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Tiles", "Home", "Desk")
	lifx.SetProductInfo(55)
	lifx.MatrixProperties.Width = 2
	lifx.MatrixProperties.Height = 1
	lifx.MatrixProperties.NZones = 2
	lifx.MatrixProperties.ChainLength = 1
	lifx.MatrixProperties.ChainZones = [][]packets.LightHsbk{{testHSBK(20), testHSBK(120)}}
	controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}}
	transport := newTestLifxTransport(t, controller)
	device := mapLifxDevice(lifx, "desk")
	device.On = true
	device.Chain[0].Pixels[0] = HSLColor{H: 20, S: 0.5, L: 0.5}
	device.Chain[0].Pixels[1] = HSLColor{H: 120, S: 0.5, L: 0.5}
	transport.storeCachedDevice(device)

	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectFlame}); err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	controller.resetSends()

	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: device}); err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want effect-off only", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect); !ok {
		t.Fatalf("first payload = %T, want *packets.TileSetEffect", controller.sentMessages()[0].msg.Payload)
	}
}

func TestLifxTransportStopFirmwareEffectRestoresPowerOffOnly(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Tiles", "Home", "Desk")
	lifx.SetProductInfo(55)
	lifx.MatrixProperties.Width = 2
	lifx.MatrixProperties.Height = 1
	lifx.MatrixProperties.NZones = 2
	lifx.MatrixProperties.ChainLength = 1
	lifx.MatrixProperties.ChainZones = [][]packets.LightHsbk{{testHSBK(20), testHSBK(120)}}
	controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}}
	transport := newTestLifxTransport(t, controller)
	device := mapLifxDevice(lifx, "desk")
	device.On = false
	device.Chain[0].Pixels[0] = HSLColor{H: 20, S: 0.5, L: 0.5}
	device.Chain[0].Pixels[1] = HSLColor{H: 120, S: 0.5, L: 0.5}
	transport.storeCachedDevice(device)

	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectFlame}); err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	controller.resetSends()

	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: device}); err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}
	if len(controller.sentMessages()) != 2 {
		t.Fatalf("sent %d messages, want power-off and effect-off", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("first payload = %T, want *packets.DeviceSetPower", controller.sentMessages()[0].msg.Payload)
	}
	if _, ok := controller.sentMessages()[1].msg.Payload.(*packets.TileSetEffect); !ok {
		t.Fatalf("second payload = %T, want *packets.TileSetEffect", controller.sentMessages()[1].msg.Payload)
	}
	if gap := controller.sentMessages()[1].at.Sub(controller.sentMessages()[0].at); gap < effectPowerOffSettleDelay {
		t.Fatalf("effect-off sent after %s, want at least %s after power-off", gap, effectPowerOffSettleDelay)
	}
}

func TestLifxTransportStopDeviceEffectRestoresOffMatrixAfterAppEffect(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Tiles", "Home", "Desk")
	lifx.SetProductInfo(55)
	lifx.MatrixProperties.Width = 2
	lifx.MatrixProperties.Height = 1
	lifx.MatrixProperties.NZones = 2
	lifx.MatrixProperties.ChainLength = 1
	lifx.MatrixProperties.ChainZones = [][]packets.LightHsbk{{testHSBK(20), testHSBK(120)}}
	controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}}
	transport := newTestLifxTransport(t, controller)
	device := mapLifxDevice(lifx, "desk")
	device.On = false
	device.Chain[0].Pixels[0] = HSLColor{H: 20, S: 0.5, L: 0.5}
	device.Chain[0].Pixels[1] = HSLColor{H: 120, S: 0.5, L: 0.5}
	transport.storeCachedDevice(device)

	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectSnake}); err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	controller.resetSends()

	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: device}); err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}
	sends := controller.sentMessages()
	if len(sends) < 2 {
		t.Fatalf("sent %d messages, want power-off and restore colors", len(sends))
	}
	if _, ok := sends[0].msg.Payload.(*packets.DeviceSetPower); !ok {
		t.Fatalf("first payload = %T, want *packets.DeviceSetPower", sends[0].msg.Payload)
	}
	payload, ok := sends[len(sends)-1].msg.Payload.(*packets.TileSet64)
	if !ok {
		t.Fatalf("last payload = %T, want *packets.TileSet64", sends[len(sends)-1].msg.Payload)
	}
	restored := lifxdevice.NewColor(payload.Colors[0])
	if restored.Hue != 20 {
		t.Fatalf("restored hue = %v, want cached hue 20", restored.Hue)
	}
}

func TestLifxTransportCloseRestoresRunningAppEffect(t *testing.T) {
	lifx := testLifxDevice(t, "d073d501a2c3", "Tiles", "Home", "Desk")
	lifx.SetProductInfo(55)
	lifx.MatrixProperties.Width = 2
	lifx.MatrixProperties.Height = 1
	lifx.MatrixProperties.NZones = 2
	lifx.MatrixProperties.ChainLength = 1
	lifx.MatrixProperties.ChainZones = [][]packets.LightHsbk{{testHSBK(20), testHSBK(120)}}
	controller := &fakeLifxController{devices: []lifxdevice.Device{lifx}, sent: make(chan sentMessage, 4)}
	transport := newTestLifxTransport(t, controller)
	device := mapLifxDevice(lifx, "desk")
	device.Chain[0].Pixels[0] = HSLColor{H: 20, S: 0.5, L: 0.5}
	device.Chain[0].Pixels[1] = HSLColor{H: 120, S: 0.5, L: 0.5}
	transport.storeCachedDevice(device)

	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: device, Effect: DeviceEffectSnake}); err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	waitForTileSet64(t, controller.sent)
	controller.resetSends()

	if err := transport.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !controller.isClosed() {
		t.Fatal("controller was not closed")
	}
	sends := controller.sentMessages()
	if len(sends) == 0 {
		t.Fatal("close did not send restore messages")
	}
	payload, ok := sends[len(sends)-1].msg.Payload.(*packets.TileSet64)
	if !ok {
		t.Fatalf("last payload = %T, want *packets.TileSet64", sends[len(sends)-1].msg.Payload)
	}
	restored := lifxdevice.NewColor(payload.Colors[0])
	if restored.Hue != 20 {
		t.Fatalf("restored hue = %v, want cached hue 20", restored.Hue)
	}
}

func TestLifxTransportStopDeviceEffectSendsMultizoneEffectOff(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindMultizone}

	status, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: device})
	if err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}
	if status.Serial != device.Serial || status.Running {
		t.Fatalf("status = %#v, want stopped status", status)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.MultiZoneSetEffect); !ok {
		t.Fatalf("payload = %T, want *packets.MultiZoneSetEffect", controller.sentMessages()[0].msg.Payload)
	}
}

func TestLifxTransportStopDeviceEffectSendsMatrixEffectOff(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindMatrix}

	status, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: device})
	if err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}
	if status.Serial != device.Serial || status.Running {
		t.Fatalf("status = %#v, want stopped status", status)
	}
	if len(controller.sentMessages()) != 1 {
		t.Fatalf("sent %d messages, want 1", len(controller.sentMessages()))
	}
	if _, ok := controller.sentMessages()[0].msg.Payload.(*packets.TileSetEffect); !ok {
		t.Fatalf("payload = %T, want *packets.TileSetEffect", controller.sentMessages()[0].msg.Payload)
	}
}

func TestLifxTransportStopDeviceEffectIsNoopForSingleZone(t *testing.T) {
	controller := &fakeLifxController{}
	transport := newTestLifxTransport(t, controller)
	device := Device{Serial: "d073d501a2c3", Kind: DeviceKindSingle}

	status, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: device})
	if err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}
	if status.Serial != device.Serial || status.Running {
		t.Fatalf("status = %#v, want stopped status", status)
	}
	if len(controller.sentMessages()) != 0 {
		t.Fatalf("sent %d messages, want 0", len(controller.sentMessages()))
	}
}

// newTestLifxTransport builds a transport and guarantees any app effect
// goroutines it starts are stopped before the test ends, so they cannot keep
// sending frames into a controller a later assertion is reading.
func newTestLifxTransport(t *testing.T, controller lifxController) *LifxTransport {
	t.Helper()
	transport := NewLifxTransportWithController(controller)
	t.Cleanup(transport.stopAllAppEffects)
	return transport
}

// fakeLifxController is shared between the test goroutine and any app effect
// goroutines the transport starts, so every field it mutates is guarded.
type fakeLifxController struct {
	mu      sync.Mutex
	devices []lifxdevice.Device
	sends   []sentMessage
	sent    chan sentMessage
	closed  bool
	now     func() time.Time
}

func (f *fakeLifxController) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeLifxController) GetDevices() []lifxdevice.Device {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.devices
}

func (f *fakeLifxController) Send(serial lifxdevice.Serial, msg *protocol.Message) error {
	f.mu.Lock()
	var at time.Time
	if f.now != nil {
		at = f.now()
	} else {
		at = time.Now()
	}
	sent := sentMessage{serial: serial, msg: msg, at: at}
	f.sends = append(f.sends, sent)
	notify := f.sent
	f.mu.Unlock()
	if notify != nil {
		select {
		case notify <- sent:
		default:
		}
	}
	return nil
}

// sentMessages returns a snapshot of the messages sent so far. Callers that
// make more than one assertion should hold on to a single snapshot: a running
// app effect keeps appending in the background.
func (f *fakeLifxController) sentMessages() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMessage(nil), f.sends...)
}

func (f *fakeLifxController) resetSends() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = nil
}

func (f *fakeLifxController) setDevices(devices []lifxdevice.Device) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devices = devices
}

func (f *fakeLifxController) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

type sentMessage struct {
	serial lifxdevice.Serial
	msg    *protocol.Message
	at     time.Time
}

func testBroadcastInterface(name string, ip string, broadcast string) lifxclient.BroadcastInterface {
	return lifxclient.BroadcastInterface{Name: name, IP: net.ParseIP(ip), Broadcast: net.ParseIP(broadcast)}
}

func testHSBK(hue float64) packets.LightHsbk {
	return packets.LightHsbk{
		Hue:        lifxdevice.ConvertExternalToDeviceValue(hue, 360),
		Saturation: lifxdevice.ConvertExternalToDeviceValue(50, 100),
		Brightness: lifxdevice.ConvertExternalToDeviceValue(50, 100),
		Kelvin:     3500,
	}
}

func assertBrightnessOnlyPayload(t *testing.T, payload *packets.LightSetWaveformOptional, brightness float64) {
	t.Helper()
	if payload.SetHue || payload.SetSaturation || payload.SetKelvin {
		t.Fatalf("payload set color fields: hue=%v saturation=%v kelvin=%v", payload.SetHue, payload.SetSaturation, payload.SetKelvin)
	}
	color := lifxdevice.NewColor(payload.Color)
	if !payload.SetBrightness || color.Brightness != brightness {
		t.Fatalf("brightness = %v/%v, want set %v", payload.SetBrightness, color.Brightness, brightness)
	}
}

func waitForSentMessage(t *testing.T, sent <-chan sentMessage) sentMessage {
	t.Helper()
	select {
	case msg := <-sent:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sent message")
		return sentMessage{}
	}
}

func waitForTileSet64(t *testing.T, sent <-chan sentMessage) *packets.TileSet64 {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case msg := <-sent:
			if payload, ok := msg.msg.Payload.(*packets.TileSet64); ok {
				return payload
			}
		case <-deadline:
			t.Fatal("timed out waiting for TileSet64")
			return nil
		}
	}
}

func waitForMultizoneSetColors(t *testing.T, sent <-chan sentMessage) *packets.MultiZoneExtendedSetColorZones {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case msg := <-sent:
			if payload, ok := msg.msg.Payload.(*packets.MultiZoneExtendedSetColorZones); ok {
				return payload
			}
		case <-deadline:
			t.Fatal("timed out waiting for MultiZoneExtendedSetColorZones")
			return nil
		}
	}
}

func testLifxDevice(t *testing.T, serialHex, label, location, group string) lifxdevice.Device {
	t.Helper()
	serial, err := lifxdevice.SerialFromHex(serialHex)
	if err != nil {
		t.Fatalf("SerialFromHex returned error: %v", err)
	}
	return lifxdevice.Device{
		Serial:    serial,
		Label:     label,
		Location:  location,
		Group:     group,
		PoweredOn: true,
		Color:     lifxdevice.Color{Brightness: 50, Kelvin: 3500},
	}
}

func locationNames(locations []Location) []string {
	names := make([]string, len(locations))
	for i, location := range locations {
		names[i] = location.Name
	}
	return names
}

func groupNames(groups []Group) []string {
	names := make([]string, len(groups))
	for i, group := range groups {
		names[i] = group.Name
	}
	return names
}

func deviceNames(devices []Device) []string {
	names := make([]string, len(devices))
	for i, device := range devices {
		names[i] = device.Name
	}
	return names
}

func assertNames(t *testing.T, label string, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %#v, want %#v", label, got, want)
		}
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

func assertPayloadHueClose(t *testing.T, color packets.LightHsbk, want float64) {
	t.Helper()
	got := lifxdevice.NewColor(color)
	if math.Abs(got.Hue-want) > 0.5 {
		t.Fatalf("hue = %v, want close to %v", got.Hue, want)
	}
}

func assertPayloadBrightness(t *testing.T, colors []packets.LightHsbk, want float64) {
	t.Helper()
	for i, color := range colors {
		got := lifxdevice.NewColor(color)
		if math.Abs(got.Brightness-want) > 0.05 {
			t.Fatalf("color %d brightness = %v, want close to %v", i, got.Brightness, want)
		}
	}
}

func TestUserFriendlyNetworkErrorMapsConnectionLoss(t *testing.T) {
	message := userFriendlyNetworkError(fmt.Errorf("check network interfaces: no network connection available"))
	if message != "Connection lost. Refresh discovery to reconnect." {
		t.Fatalf("message = %q", message)
	}
}
