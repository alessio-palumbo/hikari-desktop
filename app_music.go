package main

import (
	"context"
	"hikari-desktop/internal/backend"
	"time"
)

func (a *App) GetMusicSettings() backend.MusicSettings { return a.music.Settings() }
func (a *App) GetMusicStatus() backend.MusicStatus     { return a.music.Status() }
func (a *App) GetMusicSignal() *backend.MusicSignal    { return a.music.Signal() }
func (a *App) MusicInputs() ([]backend.MusicInputDevice, error) {
	finish, err := a.beginControl()
	if err != nil {
		return nil, err
	}
	defer finish()
	return a.music.Inputs()
}
func (a *App) StartMusic(req backend.StartMusicRequest) (backend.MusicStatus, error) {
	finish, err := a.beginControl()
	if err != nil {
		return backend.MusicStatus{}, err
	}
	defer finish()
	return a.music.Start(a.context(), req)
}
func (a *App) StopMusic(runID string) error {
	ctx, cancel := context.WithTimeout(a.context(), 25*time.Second)
	defer cancel()
	return a.music.Stop(ctx, runID)
}

func (a *App) UpdateMusicControls(controls backend.MusicControls) error {
	return a.music.UpdateControls(controls)
}
