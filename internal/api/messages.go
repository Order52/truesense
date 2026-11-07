package api

// The one-line answers that commands and the window show for an action that has no value to
// report. They are the words the 0.3.x command line used.
const (
	MsgFanAuto      = "fans: firmware auto (they stop when cool)"
	MsgFanMax       = "fans: MAX  (`truesense fan smart on` hands them back)"
	MsgFanOff       = "fans: OFF   (back on Auto automatically at CPU 70 C / GPU 65 C; `truesense fan auto` now)"
	MsgHoldReleased = "hold released: the automatic profile is in charge again"
)

// FanStopNote is added to the answer when a fan was stopped.
const FanStopNote = "   (a stopped fan: all fans return to Auto at CPU 70 C / GPU 65 C; `truesense fan auto` now. The firmware may keep the fan at its minimum speed: check `truesense fan status`)"

// ErrStopNeedsConfirm is the refusal of a fan stop that was not confirmed.
const errStopNeedsConfirm = "stopping a fan needs explicit confirmation"
