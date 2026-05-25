package backend

import (
	"context"
	"net"
	"testing"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
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

	transport := NewLifxTransportWithFactory(func() (lifxController, error) {
		return fakeLifxController{devices: []lifxdevice.Device{dev}}, nil
	})

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
	if len(got.Zones) != 1 || got.Zones[0].L != 0.5 {
		t.Fatalf("zones = %#v", got.Zones)
	}
}

func TestLifxTransportSetDeviceStateIsReadOnlyNoop(t *testing.T) {
	device := Device{ID: "test", Name: "Test"}
	transport := NewLifxTransportWithFactory(func() (lifxController, error) {
		t.Fatal("SetDeviceState should not create a controller")
		return nil, nil
	})

	got, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device})
	if err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if got.ID != device.ID {
		t.Fatalf("SetDeviceState returned %#v, want %#v", got, device)
	}
}

type fakeLifxController struct {
	devices []lifxdevice.Device
}

func (f fakeLifxController) GetDevices() []lifxdevice.Device {
	return f.devices
}
