package backend

type Location struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Group struct {
	ID         string `json:"id"`
	LocationID string `json:"locationId"`
	Name       string `json:"name"`
}

type HSLColor struct {
	H      float64 `json:"h"`
	S      float64 `json:"s"`
	L      float64 `json:"l"`
	Kelvin int     `json:"kelvin,omitempty"`
}

type MatrixRow struct {
	Cols       int     `json:"cols"`
	Offset     float64 `json:"offset"`
	HiddenCols []int   `json:"hiddenCols,omitempty"`
}

type Matrix struct {
	ID          int         `json:"id"`
	X           float64     `json:"x"`
	Y           float64     `json:"y"`
	W           float64     `json:"w"`
	H           float64     `json:"h"`
	SendWidth   int         `json:"sendWidth,omitempty"`
	Orientation int         `json:"orientation,omitempty"`
	Rows        []MatrixRow `json:"rows"`
	Pixels      []HSLColor  `json:"pixels"`
}

type Device struct {
	GroupID    string           `json:"groupId"`
	Serial     string           `json:"serial"`
	Name       string           `json:"name"`
	Model      string           `json:"model"`
	Kind       string           `json:"kind"`
	IPAddress  string           `json:"ipAddress,omitempty"`
	ProductID  uint32           `json:"productId,omitempty"`
	Firmware   string           `json:"firmware,omitempty"`
	RSSI       int              `json:"rssi,omitempty"`
	RSSIText   string           `json:"rssiText,omitempty"`
	ZoneCount  int              `json:"zoneCount,omitempty"`
	PixelCount int              `json:"pixelCount,omitempty"`
	ChainLen   int              `json:"chainLength,omitempty"`
	Online     bool             `json:"online"`
	On         bool             `json:"on"`
	Brightness float64          `json:"brightness"`
	Capability DeviceCapability `json:"capability"`
	Color      *HSLColor        `json:"color,omitempty"`
	Kelvin     int              `json:"kelvin,omitempty"`
	Zones      []HSLColor       `json:"zones,omitempty"`
	Chain      []Matrix         `json:"chain,omitempty"`
}

type DeviceCapability struct {
	HasColor  bool `json:"hasColor"`
	KelvinMin int  `json:"kelvinMin"`
	KelvinMax int  `json:"kelvinMax"`
}

type DeviceSnapshot struct {
	Locations []Location `json:"locations"`
	Groups    []Group    `json:"groups"`
	Devices   []Device   `json:"devices"`
}

func emptyDeviceSnapshot() DeviceSnapshot {
	return DeviceSnapshot{
		Locations: []Location{},
		Groups:    []Group{},
		Devices:   []Device{},
	}
}

type SetDeviceStateRequest struct {
	Device         Device `json:"device"`
	Preview        bool   `json:"preview"`
	PowerChanged   bool   `json:"powerChanged"`
	PowerOnly      bool   `json:"powerOnly"`
	BrightnessOnly bool   `json:"brightnessOnly"`
}
