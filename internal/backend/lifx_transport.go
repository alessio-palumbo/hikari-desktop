package backend

import (
	"context"
	"fmt"
	"sync"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

// lifxController is the read-only subset of lifxlan-go's controller.Controller
// used by this milestone. Keeping this tiny interface makes Snapshot mapping
// testable without starting network discovery in unit tests.
type lifxController interface {
	GetDevices() []lifxdevice.Device
}

type lifxControllerFactory func() (lifxController, error)

// LifxTransport adapts lifxlan-go behind DeviceTransport.
//
// Snapshot is read-only and maps controller.GetDevices() into the frontend DTO.
// SetDeviceState intentionally does not send packets yet. Later, state updates
// should use:
//   - power: messages.SetPowerOn / messages.SetPowerOff
//   - brightness/color/kelvin: messages.SetColor
//   - multizone: messages.SetMultizoneExtendedColors
//   - matrix: messages.SetMatrixColorsFromSlice, using MatrixProperties
//     ChainZones and ChainOrientations to map UI chain state to device order.
//
// The controller is created lazily on first Snapshot so construction stays cheap
// and tests can inject a fake controller factory.
type LifxTransport struct {
	mu         sync.Mutex
	controller lifxController
	factory    lifxControllerFactory
}

func NewLifxTransport() *LifxTransport {
	return NewLifxTransportWithFactory(func() (lifxController, error) {
		return lifxcontroller.New()
	})
}

func NewLifxTransportWithFactory(factory lifxControllerFactory) *LifxTransport {
	return &LifxTransport{factory: factory}
}

func (t *LifxTransport) Snapshot(ctx context.Context) (DeviceSnapshot, error) {
	ctrl, err := t.getController()
	if err != nil {
		return DeviceSnapshot{}, err
	}
	return mapLifxDevices(ctrl.GetDevices()), nil
}

func (t *LifxTransport) SetDeviceState(ctx context.Context, req SetDeviceStateRequest) (Device, error) {
	// Read-only milestone: keep frontend optimistic behavior, but do not send
	// packets to real devices until state synchronization is implemented.
	return req.Device, nil
}

func (t *LifxTransport) getController() (lifxController, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.controller != nil {
		return t.controller, nil
	}
	if t.factory == nil {
		return nil, fmt.Errorf("lifx controller factory is nil")
	}
	ctrl, err := t.factory()
	if err != nil {
		return nil, fmt.Errorf("create lifx controller: %w", err)
	}
	t.controller = ctrl
	return ctrl, nil
}

func mapLifxDevices(devices []lifxdevice.Device) DeviceSnapshot {
	snapshot := DeviceSnapshot{}
	locationIDs := make(map[string]bool)
	groupIDs := make(map[string]bool)

	for _, d := range devices {
		locationID := idOrUnknown(d.Location, "unknown-location")
		groupID := idOrUnknown(d.Group, "unknown-group")
		if !locationIDs[locationID] {
			snapshot.Locations = append(snapshot.Locations, Location{ID: locationID, Name: nameOrUnknown(d.Location, "Unknown")})
			locationIDs[locationID] = true
		}
		if !groupIDs[groupID] {
			snapshot.Groups = append(snapshot.Groups, Group{ID: groupID, LocationID: locationID, Name: nameOrUnknown(d.Group, "Unknown")})
			groupIDs[groupID] = true
		}
		snapshot.Devices = append(snapshot.Devices, mapLifxDevice(d, groupID))
	}

	return snapshot
}

func mapLifxDevice(d lifxdevice.Device, groupID string) Device {
	color := mapLifxColor(d.Color)
	device := Device{
		ID:         d.Serial.String(),
		GroupID:    groupID,
		Serial:     d.Serial.String(),
		Name:       nameOrUnknown(d.Label, d.Serial.String()),
		Model:      nameOrUnknown(d.RegistryName, "LIFX"),
		Kind:       mapLightKind(d.LightType.String()),
		Online:     true,
		On:         d.PoweredOn,
		Brightness: color.L,
		Color:      &color,
		Kelvin:     int(d.Color.Kelvin),
	}

	switch device.Kind {
	case "multizone":
		device.Zones = mapLifxColors(d.MultizoneProperties.Zones)
	case "matrix":
		device.Chain = mapLifxMatrixChain(d)
	}

	return device
}

func mapLifxMatrixChain(d lifxdevice.Device) []Matrix {
	props := d.MatrixProperties
	chain := make([]Matrix, 0, len(props.ChainZones))
	for i, zones := range props.ChainZones {
		chain = append(chain, Matrix{
			ID:     i,
			X:      float64(i * props.Width),
			Y:      0,
			W:      float64(props.Width),
			H:      float64(props.Height),
			Rows:   makeMatrixRows(props.Width, props.Height),
			Pixels: mapLifxColors(zones),
		})
	}
	return chain
}

func makeMatrixRows(width, height int) []MatrixRow {
	rows := make([]MatrixRow, max(0, height))
	for i := range rows {
		rows[i] = MatrixRow{Cols: width}
	}
	return rows
}

func mapLifxColors(colors []packets.LightHsbk) []HSLColor {
	mapped := make([]HSLColor, len(colors))
	for i, color := range colors {
		mapped[i] = mapLifxHSBK(color)
	}
	return mapped
}

func mapLifxColor(color lifxdevice.Color) HSLColor {
	return HSLColor{
		H: color.Hue,
		S: color.Saturation / 100,
		L: color.Brightness / 100,
	}
}

func mapLifxHSBK(color packets.LightHsbk) HSLColor {
	c := lifxdevice.NewColor(color)
	return mapLifxColor(c)
}

func mapLightKind(lightType string) string {
	switch lightType {
	case "multi_zone":
		return "multizone"
	case "matrix":
		return "matrix"
	default:
		return "single"
	}
}

func idOrUnknown(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func nameOrUnknown(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
