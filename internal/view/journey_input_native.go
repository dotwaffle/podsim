//go:build !js || !wasm

package view

type browserJourney struct{}

func (*browserJourney) update(*Game) (bool, bool) { return false, false }
