package backend

// SensorNode is the UI-facing state of a discovered Sensaa node. ID is the
// node's stable machine identity; transient network addresses are intentionally
// not exposed or persisted by Hikari.
type SensorNode struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Capabilities  []string           `json:"capabilities"`
	Online        bool               `json:"online"`
	PresenceKnown bool               `json:"presenceKnown"`
	Present       bool               `json:"present"`
	TargetCount   *SensorTargetCount `json:"targetCount,omitempty"`
}

type SensorTargetCount struct {
	Known bool `json:"known"`
	Value int  `json:"value"`
	Max   int  `json:"max,omitempty"`
}

type SensorSnapshot struct {
	Revision uint64       `json:"revision"`
	Nodes    []SensorNode `json:"nodes"`
}
