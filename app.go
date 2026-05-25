package main

import "context"

type App struct {
	ctx context.Context
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

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
	H float64 `json:"h"`
	S float64 `json:"s"`
	L float64 `json:"l"`
}

type MatrixRow struct {
	Cols   int     `json:"cols"`
	Offset float64 `json:"offset"`
}

type Matrix struct {
	ID     int         `json:"id"`
	X      float64     `json:"x"`
	Y      float64     `json:"y"`
	W      float64     `json:"w"`
	H      float64     `json:"h"`
	Rows   []MatrixRow `json:"rows"`
	Pixels []HSLColor  `json:"pixels"`
}

type Device struct {
	ID         string     `json:"id"`
	GroupID    string     `json:"groupId"`
	Serial     string     `json:"serial"`
	Name       string     `json:"name"`
	Model      string     `json:"model"`
	Kind       string     `json:"kind"`
	Online     bool       `json:"online"`
	On         bool       `json:"on"`
	Brightness float64    `json:"brightness"`
	Color      *HSLColor  `json:"color,omitempty"`
	Kelvin     int        `json:"kelvin,omitempty"`
	Zones      []HSLColor `json:"zones,omitempty"`
	Chain      []Matrix   `json:"chain,omitempty"`
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

func (a *App) GetDeviceSnapshot() DeviceSnapshot {
	return MockDeviceSnapshot()
}

func (a *App) SetDeviceState(req SetDeviceStateRequest) Device {
	// State synchronization is intentionally fake for now. The frontend updates
	// its local snapshot with the returned device, and this method marks where
	// lifxlan-go command dispatch will later live.
	return req.Device
}
