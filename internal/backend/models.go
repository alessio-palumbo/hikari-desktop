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
	ID        int         `json:"id"`
	X         float64     `json:"x"`
	Y         float64     `json:"y"`
	W         float64     `json:"w"`
	H         float64     `json:"h"`
	SendWidth int         `json:"sendWidth,omitempty"`
	Rows      []MatrixRow `json:"rows"`
	Pixels    []HSLColor  `json:"pixels"`
}

type Device struct {
	GroupID    string           `json:"groupId"`
	Serial     string           `json:"serial"`
	Name       string           `json:"name"`
	Model      string           `json:"model"`
	Kind       string           `json:"kind"`
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

type SetDeviceStateRequest struct {
	Device  Device `json:"device"`
	Preview bool   `json:"preview"`
}
