package backend

import (
	"context"
	"fmt"
	"log"
	"strings"
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

// LifxTransport adapts lifxlan-go behind DeviceTransport.
//
// Snapshot maps controller.GetDevices() into the frontend DTO. SetDeviceState
// sends power and color state for single-zone, multizone, and matrix devices.
// Matrix orientation handling is still intentionally minimal: the UI draft chain
// is sent in its current order, and MatrixProperties.ChainOrientations can be
// folded in later if the editor starts storing orientation-aware coordinates.
//
// Start creates the controller and begins lifxlan-go discovery. Tests can inject
// a fake controller directly with NewLifxTransportWithController.
type LifxTransport struct {
	controller lifxController
}

func NewLifxTransport() *LifxTransport {
	return &LifxTransport{}
}

func NewLifxTransportWithController(controller lifxController) *LifxTransport {
	return &LifxTransport{controller: controller}
}

func (t *LifxTransport) Start(ctx context.Context) error {
	if t.controller != nil {
		return nil
	}
	ctrl, err := lifxcontroller.New(
		lifxcontroller.WithHFStateRefreshPeriod(2*time.Second),
		lifxcontroller.WithLFStateRefreshPeriod(time.Minute),
		lifxcontroller.WithPreflightHandshakeTimeout(10*time.Second),
	)
	if err != nil {
		return fmt.Errorf("create lifx controller: %w", err)
	}
	log.Print("hikari: lifx controller created")
	t.controller = ctrl
	return nil
}

func (t *LifxTransport) Snapshot(ctx context.Context) (DeviceSnapshot, error) {
	ctrl, err := t.requireController()
	if err != nil {
		return DeviceSnapshot{}, err
	}
	devices := ctrl.GetDevices()
	log.Printf("hikari: lifx snapshot read %d devices", len(devices))
	return mapLifxDevices(devices), nil
}

func (t *LifxTransport) SetDeviceState(ctx context.Context, req SetDeviceStateRequest) (Device, error) {
	ctrl, err := t.requireController()
	if err != nil {
		return req.Device, err
	}
	serial, err := parseDeviceSerial(req.Device)
	if err != nil {
		return req.Device, err
	}

	if err := sendDeviceState(ctx, ctrl, serial, req.Device, req.Preview); err != nil {
		log.Printf("hikari: set device state failed for %s: %v", req.Device.Serial, err)
		return req.Device, err
	}
	return req.Device, nil
}

func (t *LifxTransport) requireController() (lifxController, error) {
	if t.controller != nil {
		return t.controller, nil
	}
	return nil, fmt.Errorf("lifx transport has not been started")
}

func mapLifxDevices(devices []lifxdevice.Device) DeviceSnapshot {
	snapshot := DeviceSnapshot{}
	locationIDs := make(map[string]bool)
	groupIDs := make(map[string]bool)

	for _, d := range devices {
		if !isSupportedLightDevice(d) {
			log.Printf("hikari: skipping unsupported lifx device %s type %q", d.Serial.String(), d.Type.String())
			continue
		}
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

func isSupportedLightDevice(d lifxdevice.Device) bool {
	switch d.Type.String() {
	case "", "light", "hybrid":
		return true
	default:
		return false
	}
}

func mapLifxDevice(d lifxdevice.Device, groupID string) Device {
	capability := mapLifxCapability(d.ColorProperties)
	color := mapLifxColor(d.Color, capability)
	kelvin := int(d.Color.Kelvin)
	if kelvin <= 0 && isFixedKelvin(capability) {
		kelvin = capability.KelvinMin
	}
	device := Device{
		GroupID:    groupID,
		Serial:     d.Serial.String(),
		Name:       nameOrUnknown(d.Label, d.Serial.String()),
		Model:      nameOrUnknown(d.RegistryName, "LIFX"),
		Kind:       mapLightKind(d.LightType.String()),
		Online:     true,
		On:         d.PoweredOn,
		Brightness: color.L,
		Capability: capability,
		Color:      &color,
		Kelvin:     kelvin,
	}

	switch device.Kind {
	case "multizone":
		device.Zones = mapLifxColors(d.MultizoneProperties.Zones, capability)
	case "matrix":
		device.Chain = mapLifxMatrixChain(d, capability)
	}

	return device
}

func mapLifxMatrixChain(d lifxdevice.Device, capability DeviceCapability) []Matrix {
	props := d.MatrixProperties
	chain := make([]Matrix, 0, len(props.ChainZones))
	for i, zones := range props.ChainZones {
		rows := makeMatrixRowsForDevice(d, len(zones))
		displayWidth := matrixRowsWidth(rows, props.Width)
		chain = append(chain, Matrix{
			ID:        i,
			X:         float64(i) * displayWidth,
			Y:         0,
			W:         displayWidth,
			H:         float64(len(rows)),
			SendWidth: props.Width,
			Rows:      rows,
			Pixels:    mapLifxColors(zones, capability),
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

func makeMatrixRowsForDevice(d lifxdevice.Device, pixels int) []MatrixRow {
	width, height := displayMatrixDimensions(d, pixels)
	rows := makeMatrixRows(width, height)
	if len(rows) == 0 {
		return rows
	}
	hiddenCols := hiddenMatrixColsByRow(d.ProductID, width)
	for rowIndex, cols := range hiddenCols {
		if rowIndex >= len(rows) {
			continue
		}
		rows[rowIndex].HiddenCols = cols
	}
	if candleProducts[d.ProductID] && len(rows) > 0 {
		rows[0].Offset = 1
	}
	return rows
}

func displayMatrixDimensions(d lifxdevice.Device, pixels int) (width, height int) {
	width = d.MatrixProperties.Width
	height = d.MatrixProperties.Height
	if ceilingCapsuleProducts[d.ProductID] && pixels%16 == 0 {
		width = 16
		height = pixels / width
	}
	return width, height
}

func matrixRowsWidth(rows []MatrixRow, fallback int) float64 {
	width := fallback
	for _, row := range rows {
		width = max(width, int(row.Offset)+row.Cols)
	}
	return float64(width)
}

func hiddenMatrixColsByRow(productID uint32, width int) map[int][]int {
	hidden := hiddenMatrixIndexes(productID)
	if len(hidden) == 0 || width <= 0 {
		return nil
	}
	byRow := make(map[int][]int)
	for _, index := range hidden {
		byRow[index/width] = append(byRow[index/width], index%width)
	}
	return byRow
}

func hiddenMatrixIndexes(productID uint32) []int {
	switch {
	case candleProducts[productID]:
		return []int{2, 3, 4}
	case ceilingProducts[productID]:
		return []int{0, 1, 6, 7, 56, 57, 62, 63}
	case ceilingCapsuleProducts[productID]:
		return []int{0, 1, 14, 15, 112, 113, 126, 127}
	case lunaProducts[productID]:
		return []int{0, 6, 28, 34}
	default:
		return nil
	}
}

var candleProducts = map[uint32]bool{
	57: true, 67: true, 68: true, 81: true, 96: true, 137: true, 138: true, 185: true, 186: true, 187: true, 188: true, 215: true, 216: true, 217: true, 218: true,
}

var ceilingProducts = map[uint32]bool{
	145: true, 146: true, 176: true, 177: true, 265: true, 266: true,
}

var ceilingCapsuleProducts = map[uint32]bool{
	201: true, 202: true,
}

var lunaProducts = map[uint32]bool{
	219: true, 220: true,
}

func mapLifxColors(colors []packets.LightHsbk, capability DeviceCapability) []HSLColor {
	mapped := make([]HSLColor, len(colors))
	for i, color := range colors {
		mapped[i] = mapLifxHSBK(color, capability)
	}
	return mapped
}

func mapLifxColor(color lifxdevice.Color, capability DeviceCapability) HSLColor {
	mapped := HSLColor{
		H: color.Hue,
		S: color.Saturation / 100,
		L: color.Brightness / 100,
	}
	kelvin := int(color.Kelvin)
	if kelvin <= 0 && isFixedKelvin(capability) {
		kelvin = capability.KelvinMin
	}
	if kelvin > 0 && (!deviceHasColor(capability) || isFixedKelvin(capability) || color.Saturation <= 0.5) {
		mapped.H = 0
		mapped.S = 0
		mapped.Kelvin = kelvin
	}
	return mapped
}

func mapLifxHSBK(color packets.LightHsbk, capability DeviceCapability) HSLColor {
	c := lifxdevice.NewColor(color)
	return mapLifxColor(c, capability)
}

func mapLifxCapability(props lifxdevice.ColorProperties) DeviceCapability {
	minKelvin := props.TemperatureRange.Min
	maxKelvin := props.TemperatureRange.Max
	if minKelvin <= 0 {
		minKelvin = 1500
	}
	if maxKelvin <= 0 {
		maxKelvin = 9000
	}
	return DeviceCapability{
		HasColor:  props.HasColor,
		KelvinMin: minKelvin,
		KelvinMax: maxKelvin,
	}
}

func parseDeviceSerial(device Device) (lifxdevice.Serial, error) {
	serial := device.Serial
	serial = strings.ReplaceAll(serial, ":", "")
	return lifxdevice.SerialFromHex(serial)
}

func sendDeviceState(ctx context.Context, ctrl lifxController, serial lifxdevice.Serial, device Device, direct bool) error {
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

	for _, msg := range deviceStateMessages(device, direct) {
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

func deviceStateMessages(device Device, direct bool) []*protocol.Message {
	switch device.Kind {
	case "single":
		return []*protocol.Message{singleZoneColorMessage(device)}
	case "multizone":
		if direct {
			return []*protocol.Message{singleZoneColorMessage(device)}
		}
		return messages.SetMultizoneExtendedColors(0, hslColorsToHSBK(device.Zones, device.Brightness, device.Kelvin, device.Capability), time.Millisecond)
	case "matrix":
		if direct {
			return []*protocol.Message{singleZoneColorMessage(device)}
		}
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
				hslColorsToHSBK(matrix.Pixels, device.Brightness, device.Kelvin, device.Capability),
				time.Millisecond,
			)...)
		}
		return msgs
	default:
		return nil
	}
}

func matrixWidth(matrix Matrix) int {
	if matrix.SendWidth > 0 {
		return matrix.SendWidth
	}
	for _, row := range matrix.Rows {
		if row.Cols > 0 {
			return row.Cols
		}
	}
	return int(matrix.W)
}

func hslColorsToHSBK(colors []HSLColor, brightness float64, kelvin int, capability DeviceCapability) []packets.LightHsbk {
	mapped := make([]packets.LightHsbk, len(colors))
	for i, color := range colors {
		mapped[i] = hslColorToHSBK(color, brightness, kelvin, capability)
	}
	return mapped
}

func hslColorToHSBK(color HSLColor, brightness float64, kelvin int, capability DeviceCapability) packets.LightHsbk {
	brightness = color.L
	if kelvin <= 0 {
		kelvin = 3500
	}
	if color.Kelvin > 0 {
		kelvin = color.Kelvin
		color.S = 0
	} else if !deviceHasColor(capability) {
		color.H = 0
		color.S = 0
	}
	kelvin = clampKelvin(kelvin, deviceKelvinMin(capability), deviceKelvinMax(capability))
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
	if device.Color != nil && deviceHasColor(device.Capability) {
		s := clamp(device.Color.S, 0, 1) * 100
		saturation = &s
		if device.Color.Kelvin == 0 && device.Color.S > 0 {
			h := clamp(device.Color.H, 0, 360)
			hue = &h
		}
	}
	var kelvin *uint16
	if device.Kelvin > 0 || device.Color != nil && device.Color.Kelvin > 0 {
		value := device.Kelvin
		if device.Color != nil && device.Color.Kelvin > 0 {
			value = device.Color.Kelvin
		}
		k := uint16(clampKelvin(value, deviceKelvinMin(device.Capability), deviceKelvinMax(device.Capability)))
		kelvin = &k
	}
	return messages.SetColor(hue, saturation, &brightness, kelvin, time.Millisecond, 0)
}

func deviceKelvinMin(capability DeviceCapability) int {
	if capability.KelvinMin > 0 {
		return capability.KelvinMin
	}
	return 1500
}

func deviceHasColor(capability DeviceCapability) bool {
	if capability.HasColor {
		return true
	}
	return capability.KelvinMin == 0 && capability.KelvinMax == 0
}

func isFixedKelvin(capability DeviceCapability) bool {
	return capability.KelvinMin > 0 && capability.KelvinMin == capability.KelvinMax
}

func deviceKelvinMax(capability DeviceCapability) int {
	if capability.KelvinMax > 0 {
		return capability.KelvinMax
	}
	return 9000
}

func clampKelvin(kelvin, minKelvin, maxKelvin int) int {
	if maxKelvin < minKelvin {
		maxKelvin = minKelvin
	}
	if kelvin < minKelvin {
		return minKelvin
	}
	if kelvin > maxKelvin {
		return maxKelvin
	}
	return kelvin
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
