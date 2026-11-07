// Package tui is the terminal control surface (`truesense tui`): every setting of the
// laptop on seven pages, usable over SSH and on a bare console.
//
// The package is layered so each layer is testable without a terminal:
//
//	Glyphs, Canvas   what is drawn on (a text grid in the tests, a tcell screen in the program)
//	Data             the latest readings, refreshed in the background; the screen never waits for them
//	Row              one setting (choice, toggle, slider, button, info) and how it changes
//	Worker           hardware writes: one at a time, in order, a newer write for a key replaces an unstarted one
//	App              pages, keys, mouse, dialogs, footer: a state machine driven by Key/Mouse events
//	Run              the tcell event loop (run.go)
//
// The package imports no domain package. Everything it reads or writes goes through the
// Backend interface declared here, in neutral types, so the program is tested with a fake
// laptop and wired to the real packages by an adapter written elsewhere.
//
// Rules the whole screen follows: a value is never written that the user did not choose;
// stopping a fan (or anything that needs a restart) asks first and the answer defaults to
// No; writes happen one at a time in order; a value that fails to write goes back to what
// the hardware has.
package tui
