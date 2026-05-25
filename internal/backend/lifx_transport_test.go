package backend

import (
	"context"
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

	transport := NewLifxTransportWithFactory(func() (lifxController, error) {
		return &fakeLifxController{devices: []lifxdevice.Device{dev}}, nil
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

func TestLifxTransportSetDeviceStateSendsSingleZonePowerAndColor(t *testing.T) {
	controller := &fakeLifxController{}
	device := Device{
		ID:         "d073d501a2c3",
		Serial:     "d073d501a2c3",
		Name:       "Test",
		Kind:       "single",
		On:         true,
		Brightness: 0.42,
		Color:      &HSLColor{H: 210, S: 0.75, L: 0.6},
		Kelvin:     4000,
	}
	transport := NewLifxTransportWithFactory(func() (lifxController, error) {
		return controller, nil
	})

	got, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device})
	if err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if got.ID != device.ID {
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
	device := Device{ID: "d073d501a2c3", Serial: "d073d501a2c3", Kind: "single", On: false}
	transport := NewLifxTransportWithFactory(func() (lifxController, error) {
		return controller, nil
	})

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

func TestLifxTransportSetDeviceStateKeepsMultizoneAndMatrixReadOnly(t *testing.T) {
	transport := NewLifxTransportWithFactory(func() (lifxController, error) {
		t.Fatal("non-single SetDeviceState should not create a controller")
		return nil, nil
	})

	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: Device{ID: "strip", Kind: "multizone"}}); err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
}

type fakeLifxController struct {
	devices []lifxdevice.Device
	sends   []sentMessage
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
