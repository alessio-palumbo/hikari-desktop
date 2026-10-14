//go:build cgo && (darwin || linux || windows)

package backend

import (
	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
	"github.com/alessio-palumbo/lifx-maestro/live/microphone"
)

func musicCaptureSupported() bool { return true }
func newMusicMicrophone(id string) (maestrolive.AudioSource, error) {
	return microphone.NewMicrophoneSource(microphone.MicrophoneConfig{Device: id}), nil
}
func listMusicMicrophones() ([]MusicInputDevice, error) {
	inputs, err := microphone.ListInputDevices()
	if err != nil {
		return nil, err
	}
	result := make([]MusicInputDevice, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, MusicInputDevice{ID: input.ID, Name: input.Name, Default: input.Default})
	}
	return result, nil
}
