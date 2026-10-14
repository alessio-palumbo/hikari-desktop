//go:build !cgo || (!darwin && !linux && !windows)

package backend

import (
	"errors"
	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
)

func musicCaptureSupported() bool { return false }
func newMusicMicrophone(string) (maestrolive.AudioSource, error) {
	return nil, errors.New("Music capture is unavailable in this build")
}
func listMusicMicrophones() ([]MusicInputDevice, error) {
	return []MusicInputDevice{}, errors.New("Music capture is unavailable in this build")
}
