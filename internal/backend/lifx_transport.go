package backend

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxlan-go/pkg/messages"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

// lifxController is the subset of lifxlan-go's controller.Controller used by
// the transport. Keeping this tiny interface makes mapping and sends testable
// without starting network discovery in unit tests.
type lifxController interface {
	GetDevices() []lifxdevice.Device
	Send(lifxdevice.Serial, *protocol.Message) error
}

type lifxControllerFactory func() (lifxController, error)

// LifxTransport adapts lifxlan-go behind DeviceTransport.
//
// Snapshot maps controller.GetDevices() into the frontend DTO. SetDeviceState
// sends power and color state for single-zone, multizone, and matrix devices.
// Matrix orientation handling is still intentionally minimal: the UI draft chain
// is sent in its current order, and MatrixProperties.ChainOrientations can be
// folded in later if the editor starts storing orientation-aware coordinates.
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
	ctrl, err := t.getController()
	if err != nil {
		return req.Device, err
	}
	serial, err := parseDeviceSerial(req.Device)
	if err != nil {
		return req.Device, err
	}

	if err := sendDeviceState(ctx, ctrl, serial, req.Device); err != nil {
		return req.Device, err
	}
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

func parseDeviceSerial(device Device) (lifxdevice.Serial, error) {
	serial := device.Serial
	serial = strings.ReplaceAll(serial, ":", "")
	return lifxdevice.SerialFromHex(serial)
}

func sendDeviceState(ctx context.Context, ctrl lifxController, serial lifxdevice.Serial, device Device) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if !device.On {
		if err := ctrl.Send(serial, messages.SetPowerOff()); err != nil {
			return fmt.Errorf("set power off: %w", err)
		}
		return nil
	}

	if err := ctrl.Send(serial, messages.SetPowerOn()); err != nil {
		return fmt.Errorf("set power on: %w", err)
	}

	for _, msg := range deviceStateMessages(device) {
		if msg == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ctrl.Send(serial, msg); err != nil {
			return fmt.Errorf("set %s state: %w", device.Kind, err)
		}
	}

	return nil
}

func deviceStateMessages(device Device) []*protocol.Message {
	switch device.Kind {
	case "single":
		return []*protocol.Message{singleZoneColorMessage(device)}
	case "multizone":
		return messages.SetMultizoneExtendedColors(0, hslColorsToHSBK(device.Zones, device.Brightness, device.Kelvin), time.Millisecond)
	case "matrix":
		msgs := make([]*protocol.Message, 0)
		for _, matrix := range device.Chain {
			width := matrixWidth(matrix)
			if width <= 0 {
				continue
			}
			msgs = append(msgs, messages.SetMatrixColorsFromSlice(
				matrix.ID,
				len(device.Chain),
				width,
				hslColorsToHSBK(matrix.Pixels, device.Brightness, device.Kelvin),
				time.Millisecond,
			)...)
		}
		return msgs
	default:
		return nil
	}
}

func matrixWidth(matrix Matrix) int {
	for _, row := range matrix.Rows {
		if row.Cols > 0 {
			return row.Cols
		}
	}
	return int(matrix.W)
}

func hslColorsToHSBK(colors []HSLColor, brightness float64, kelvin int) []packets.LightHsbk {
	mapped := make([]packets.LightHsbk, len(colors))
	for i, color := range colors {
		mapped[i] = hslColorToHSBK(color, brightness, kelvin)
	}
	return mapped
}

func hslColorToHSBK(color HSLColor, brightness float64, kelvin int) packets.LightHsbk {
	if brightness <= 0 {
		brightness = color.L
	}
	if kelvin <= 0 {
		kelvin = 3500
	}
	return lifxdevice.Color{
		Hue:        clamp(color.H, 0, 360),
		Saturation: clamp(color.S, 0, 1) * 100,
		Brightness: clamp(brightness, 0, 1) * 100,
		Kelvin:     uint16(clamp(float64(kelvin), 1500, 9000)),
	}.ToDeviceColor()
}

func singleZoneColorMessage(device Device) *protocol.Message {
	brightness := clamp(device.Brightness, 0, 1) * 100
	var hue, saturation *float64
	if device.Color != nil {
		h := clamp(device.Color.H, 0, 360)
		s := clamp(device.Color.S, 0, 1) * 100
		hue = &h
		saturation = &s
	}
	var kelvin *uint16
	if device.Kelvin > 0 {
		k := uint16(clamp(float64(device.Kelvin), 1500, 9000))
		kelvin = &k
	}
	return messages.SetColor(hue, saturation, &brightness, kelvin, time.Millisecond, 0)
}

func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
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
