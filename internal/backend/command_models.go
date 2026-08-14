package backend

type CommandEngineSettings struct {
	Enabled       bool   `json:"enabled"`
	EnginePath    string `json:"enginePath,omitempty"`
	ConfigPath    string `json:"configPath,omitempty"`
	Available     bool   `json:"available"`
	Transcription bool   `json:"transcription"`
	Warning       string `json:"warning,omitempty"`
}

type SetCommandEngineSettingsRequest struct {
	Enabled    bool   `json:"enabled"`
	EnginePath string `json:"enginePath,omitempty"`
	ConfigPath string `json:"configPath,omitempty"`
}

type InterpretCommandRequest struct {
	Text string `json:"text"`
}

type TranscribeCommandRequest struct {
	AudioPath string `json:"audioPath"`
	Language  string `json:"language,omitempty"`
}

type TranscribeCommandAudioRequest struct {
	AudioBase64 string `json:"audioBase64"`
	Language    string `json:"language,omitempty"`
}

type SpeechCommandPreview struct {
	Transcript CommandTranscript `json:"transcript"`
	Preview    CommandPreview    `json:"preview"`
}

type CommandTranscript struct {
	Text     string                     `json:"text"`
	Language string                     `json:"language,omitempty"`
	Segments []CommandTranscriptSegment `json:"segments"`
}

type CommandTranscriptSegment struct {
	StartMS int64  `json:"startMs"`
	EndMS   int64  `json:"endMs"`
	Text    string `json:"text"`
}

type CommandPreview struct {
	Summary           string                  `json:"summary"`
	Confidence        float64                 `json:"confidence"`
	ConfidenceLevel   string                  `json:"confidenceLevel,omitempty"`
	Reasons           []string                `json:"reasons"`
	NeedsConfirmation bool                    `json:"needsConfirmation"`
	Empty             bool                    `json:"empty"`
	Commands          []CommandPreviewCommand `json:"commands"`
	SkippedTargets    []CommandPreviewTarget  `json:"skippedTargets"`
}

type CommandPreviewCommand struct {
	Targets []CommandPreviewTarget `json:"targets"`
	Action  CommandPreviewAction   `json:"action"`
}

type CommandPreviewTarget struct {
	Serial   string `json:"serial"`
	Label    string `json:"label,omitempty"`
	Group    string `json:"group,omitempty"`
	Location string `json:"location,omitempty"`
}

type CommandPreviewAction struct {
	Power      *bool    `json:"power,omitempty"`
	Hue        *float64 `json:"hue,omitempty"`
	Saturation *float64 `json:"saturation,omitempty"`
	Brightness *float64 `json:"brightness,omitempty"`
	Kelvin     *int     `json:"kelvin,omitempty"`
	DurationMS *int     `json:"durationMs,omitempty"`
}
