package backend

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxlan-go/pkg/messages"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

const defaultColorTransitionDuration = 300 * time.Millisecond

// lifxController is the subset of lifxlan-go's controller.Controller used by
// the transport. Keeping this tiny interface makes mapping and sends testable
// without starting network discovery in unit tests.
type lifxController interface {
	Close() error
	GetDevices() []lifxdevice.Device
	Send(lifxdevice.Serial, *protocol.Message) error
}

// LifxTransport adapts lifxlan-go behind DeviceTransport.
//
// Snapshot maps controller.GetDevices() into the frontend DTO. SetDeviceState
// sends power and color state for single-zone, multizone, and matrix devices.
// Matrix colors are rotated into UI orientation for previews and rotated back
// to device order when applying edited pixels.
//
// Start creates the controller and begins lifxlan-go discovery. Tests can inject
// a fake controller directly with NewLifxTransportWithController.
type LifxTransport struct {
	controller lifxController
	mu         sync.RWMutex
	cache      map[string]Device
}

func NewLifxTransport() *LifxTransport {
	return &LifxTransport{cache: make(map[string]Device)}
}

func NewLifxTransportWithController(controller lifxController) *LifxTransport {
	return &LifxTransport{controller: controller, cache: make(map[string]Device)}
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

func (t *LifxTransport) Close(ctx context.Context) error {
	if t.controller == nil {
		return nil
	}
	if err := t.controller.Close(); err != nil {
		return fmt.Errorf("close lifx controller: %w", err)
	}
	t.controller = nil
	log.Print("hikari: lifx controller closed")
	return nil
}

func (t *LifxTransport) Snapshot(ctx context.Context) (DeviceSnapshot, error) {
	ctrl, err := t.requireController()
	if err != nil {
		return DeviceSnapshot{}, err
	}
	devices := ctrl.GetDevices()
	log.Printf("hikari: lifx snapshot read %d devices", len(devices))
	snapshot := mapLifxDevices(devices)
	t.updateCache(snapshot.Devices)
	return snapshot, nil
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

	current := t.cachedDevice(req.Device.Serial)
	if current == nil {
		snapshot := mapLifxDevices(ctrl.GetDevices())
		t.updateCache(snapshot.Devices)
		current = t.cachedDevice(req.Device.Serial)
	}

	intent := normalizeDeviceCommandIntent(req.Intent, req.Device)
	if err := sendDeviceState(ctx, ctrl, serial, req.Device, req.Preview, intent, current); err != nil {
		log.Printf("hikari: set device state failed for %s: %v", req.Device.Serial, err)
		return req.Device, err
	}
	t.updateCache([]Device{req.Device})
	return req.Device, nil
}

func (t *LifxTransport) requireController() (lifxController, error) {
	if t.controller != nil {
		return t.controller, nil
	}
	return nil, fmt.Errorf("lifx transport has not been started")
}

func (t *LifxTransport) updateCache(devices []Device) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cache == nil {
		t.cache = make(map[string]Device)
	}
	for _, device := range devices {
		t.cache[device.Serial] = device
	}
}

func (t *LifxTransport) cachedDevice(serial string) *Device {
	t.mu.RLock()
	defer t.mu.RUnlock()
	device, ok := t.cache[serial]
	if !ok {
		return nil
	}
	return &device
}

func mapLifxDevices(devices []lifxdevice.Device) DeviceSnapshot {
	snapshot := emptyDeviceSnapshot()
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

	return sortDeviceSnapshot(snapshot)
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
		IPAddress:  deviceIPAddress(d),
		ProductID:  d.ProductID,
		Firmware:   d.FirmwareVersion,
		RSSI:       int(d.WifiRSSI),
		RSSIText:   d.WifiRSSI.String(),
		Online:     true,
		On:         d.PoweredOn,
		Brightness: color.L,
		Capability: capability,
		Color:      &color,
		Kelvin:     kelvin,
	}

	switch device.Kind {
	case "multizone":
		device.ZoneCount = len(d.MultizoneProperties.Zones)
		device.Zones = mapLifxColors(d.MultizoneProperties.Zones, capability)
		applyColorSummary(&device, device.Zones)
	case "matrix":
		device.PixelCount = d.MatrixProperties.NZones
		device.ChainLen = d.MatrixProperties.ChainLength
		if device.ChainLen == 0 {
			device.ChainLen = len(d.MatrixProperties.ChainZones)
		}
		device.Chain = mapLifxMatrixChain(d, capability)
		applyColorSummary(&device, matrixPixels(device.Chain))
	}

	return device
}

func applyColorSummary(device *Device, colors []HSLColor) {
	if len(colors) == 0 {
		return
	}
	summary := averageHSLColor(colors)
	device.Brightness = summary.L
	device.Color = &summary
	if summary.Kelvin > 0 {
		device.Kelvin = summary.Kelvin
	}
}

func matrixPixels(chain []Matrix) []HSLColor {
	var colors []HSLColor
	for _, matrix := range chain {
		colors = append(colors, matrix.Pixels...)
	}
	return colors
}

func deviceIPAddress(d lifxdevice.Device) string {
	if d.Address == nil || d.Address.IP == nil {
		return ""
	}
	return d.Address.IP.String()
}

func mapLifxMatrixChain(d lifxdevice.Device, capability DeviceCapability) []Matrix {
	surface := lifxdevice.SurfaceFromDevice(d)
	if surface.Matrix == nil {
		return nil
	}

	chain := make([]Matrix, 0, len(d.MatrixProperties.ChainZones))
	for i, zones := range d.MatrixProperties.ChainZones {
		if i >= len(surface.Matrix.Chains) {
			break
		}
		surfaceChain := surface.Matrix.Chains[i]
		sendWidth := surfaceChain.SendWidth
		sendHeight := matrixSendHeight(sendWidth, len(zones), surfaceChain.Bounds.Height)
		chain = append(chain, Matrix{
			ID:          surfaceChain.Index,
			X:           float64(surfaceChain.Bounds.X),
			Y:           float64(surfaceChain.Bounds.Y),
			W:           float64(surfaceChain.Bounds.Width),
			H:           float64(surfaceChain.Bounds.Height),
			SendWidth:   sendWidth,
			Orientation: int(surfaceChain.Orientation),
			Rows:        mapSurfaceRows(surfaceChain.Rows),
			Pixels:      mapLifxColors(adjustUIGridForOrientation(sendWidth, sendHeight, surfaceChain.Orientation, zones), capability),
		})
	}
	return chain
}

func mapSurfaceRows(rows []lifxdevice.MatrixRow) []MatrixRow {
	mapped := make([]MatrixRow, len(rows))
	for i, row := range rows {
		mapped[i] = MatrixRow{
			Cols:       row.Cols,
			Offset:     float64(row.Offset),
			HiddenCols: append([]int(nil), row.HiddenCols...),
		}
	}
	return mapped
}

func matrixSendHeight(sendWidth int, pixels int, fallback int) int {
	if sendWidth > 0 && pixels > 0 {
		return max((pixels+sendWidth-1)/sendWidth, 1)
	}
	return max(fallback, 1)
}

func adjustUIGridForOrientation(width, height int, orientation lifxdevice.Orientation, colors []packets.LightHsbk) []packets.LightHsbk {
	switch orientation {
	case lifxdevice.OrientationRight:
		return lifxdevice.RotateMatrix(lifxdevice.RotateMatrix90(width, height), colors)
	case lifxdevice.OrientationUpsideDown:
		return lifxdevice.RotateMatrix(lifxdevice.RotateMatrix180(width, height), colors)
	case lifxdevice.OrientationLeft:
		return lifxdevice.RotateMatrix(lifxdevice.RotateMatrix270(width, height), colors)
	default:
		return colors
	}
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

func averageHSLColor(colors []HSLColor) HSLColor {
	var hueX, hueY, saturation, lightness float64
	var kelvinSum, kelvinCount int
	for _, color := range colors {
		radians := color.H * math.Pi / 180
		weight := math.Max(color.S, 0.01)
		hueX += math.Cos(radians) * weight
		hueY += math.Sin(radians) * weight
		saturation += color.S
		lightness += color.L
		if color.Kelvin > 0 {
			kelvinSum += color.Kelvin
			kelvinCount++
		}
	}

	count := float64(len(colors))
	average := HSLColor{
		H: averageHue(hueX, hueY),
		S: saturation / count,
		L: lightness / count,
	}
	if kelvinCount > 0 {
		average.Kelvin = int(math.Round(float64(kelvinSum) / float64(kelvinCount)))
		if average.S <= 0.005 {
			average.H = 0
			average.S = 0
		}
	}
	return average
}

func averageHue(x, y float64) float64 {
	if math.Abs(x) < 1e-9 && math.Abs(y) < 1e-9 {
		return 0
	}
	hue := math.Atan2(y, x) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}
	return hue
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

func sendDeviceState(ctx context.Context, ctrl lifxController, serial lifxdevice.Serial, device Device, direct bool, intent DeviceCommandIntent, current *Device) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	logLifxRequest(serial, device, direct, intent, current)

	if intent == DeviceCommandPower {
		if device.On {
			msg := messages.SetPowerOn()
			logLifxSend(serial, device, "power-on", msg)
			if err := ctrl.Send(serial, msg); err != nil {
				return fmt.Errorf("set power on: %w", err)
			}
			return nil
		}
		msg := messages.SetPowerOff()
		logLifxSend(serial, device, "power-off", msg)
		if err := ctrl.Send(serial, msg); err != nil {
			return fmt.Errorf("set power off: %w", err)
		}
		return nil
	}

	if !device.On {
		if current != nil && current.On {
			msg := messages.SetPowerOff()
			logLifxSend(serial, device, "power-off", msg)
			if err := ctrl.Send(serial, msg); err != nil {
				return fmt.Errorf("set power off: %w", err)
			}
		}
		return nil
	}

	if current != nil && !current.On {
		powerMsg := messages.SetPowerOn()
		logLifxSend(serial, device, "power-on", powerMsg)
		if err := ctrl.Send(serial, powerMsg); err != nil {
			return fmt.Errorf("set power on: %w", err)
		}
	}
	if intent == DeviceCommandBrightness {
		msg := brightnessOnlyMessage(device)
		logLifxSend(serial, device, "brightness-only", msg)
		if err := ctrl.Send(serial, msg); err != nil {
			return fmt.Errorf("set brightness: %w", err)
		}
		return nil
	}

	for index, msg := range deviceStateMessages(device, direct) {
		if msg == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		logLifxSend(serial, device, fmt.Sprintf("state-%d", index+1), msg)
		if err := ctrl.Send(serial, msg); err != nil {
			return fmt.Errorf("set %s state: %w", device.Kind, err)
		}
	}

	return nil
}

func normalizeDeviceCommandIntent(intent DeviceCommandIntent, device Device) DeviceCommandIntent {
	switch intent {
	case DeviceCommandPower, DeviceCommandBrightness, DeviceCommandColor, DeviceCommandZones, DeviceCommandMatrix:
		return intent
	}
	switch device.Kind {
	case "multizone":
		return DeviceCommandZones
	case "matrix":
		return DeviceCommandMatrix
	default:
		return DeviceCommandColor
	}
}

func deviceStateMessages(device Device, direct bool) []*protocol.Message {
	switch device.Kind {
	case "single":
		return []*protocol.Message{singleZoneColorMessage(device)}
	case "multizone":
		if direct {
			return []*protocol.Message{singleZoneColorMessage(device)}
		}
		return messages.SetMultizoneExtendedColors(0, hslColorsToHSBK(device.Zones, device.Brightness, device.Kelvin, device.Capability), defaultColorTransitionDuration)
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
				rotateMatrixForOrientation(matrix, hslColorsToHSBK(matrix.Pixels, device.Brightness, device.Kelvin, device.Capability)),
				defaultColorTransitionDuration,
			)...)
		}
		return msgs
	default:
		return nil
	}
}

func rotateMatrixForOrientation(matrix Matrix, colors []packets.LightHsbk) []packets.LightHsbk {
	width := matrixWidth(matrix)
	height := matrixHeight(matrix)
	if width <= 0 || height <= 0 {
		return colors
	}
	return lifxdevice.ReorientMatrix(width, height, lifxdevice.Orientation(matrix.Orientation), colors)
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

func matrixHeight(matrix Matrix) int {
	if matrix.SendWidth > 0 && matrix.SendWidth <= len(matrix.Pixels) {
		return len(matrix.Pixels) / matrix.SendWidth
	}
	if len(matrix.Rows) > 0 {
		return len(matrix.Rows)
	}
	return int(matrix.H)
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
	return messages.SetColor(hue, saturation, &brightness, kelvin, defaultColorTransitionDuration, 0)
}

func brightnessOnlyMessage(device Device) *protocol.Message {
	brightness := clamp(device.Brightness, 0, 1) * 100
	return messages.SetColor(nil, nil, &brightness, nil, defaultColorTransitionDuration, 0)
}

func logLifxRequest(serial lifxdevice.Serial, device Device, direct bool, intent DeviceCommandIntent, current *Device) {
	if !lifxDebugEnabled() {
		return
	}
	currentPower := "unknown"
	if current != nil {
		currentPower = fmt.Sprintf("%v", current.On)
	}
	log.Printf(
		"hikari: lifx request serial=%s name=%q kind=%s intent=%s on=%v currentOn=%s brightness=%.2f direct=%v",
		serial.String(),
		device.Name,
		device.Kind,
		intent,
		device.On,
		currentPower,
		device.Brightness,
		direct,
	)
}

func logLifxSend(serial lifxdevice.Serial, device Device, action string, msg *protocol.Message) {
	if !lifxDebugEnabled() {
		return
	}
	payload := "<nil>"
	if msg != nil && msg.Payload != nil {
		payload = fmt.Sprintf("%T", msg.Payload)
	}
	log.Printf("hikari: lifx send serial=%s kind=%s action=%s payload=%s", serial.String(), device.Kind, action, payload)
}

func lifxDebugEnabled() bool {
	level := strings.ToLower(os.Getenv("HIKARI_LOG_LEVEL"))
	return level == "debug" || level == "trace"
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
