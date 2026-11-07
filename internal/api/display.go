package api

// Overdrive reports the LCD overdrive; nil when the firmware cannot say.
func (a *API) Overdrive() (*bool, error) { return optionalBool(a.s.HW.GetOverdrive()) }

// SetOverdrive switches the LCD overdrive (verified by read-back).
func (a *API) SetOverdrive(on bool) (bool, error) { return a.s.HW.SetOverdrive(on) }

// BootSound reports the firmware's start-up chime.
func (a *API) BootSound() (bool, error) { return a.s.HW.GetBootSound() }

// SetBootSound switches the start-up chime (verified by read-back).
func (a *API) SetBootSound(on bool) (bool, error) { return a.s.HW.SetBootSound(on) }

// KeyboardTimeout reports "backlight off after 30 s of no typing".
func (a *API) KeyboardTimeout() (bool, error) { return a.s.HW.GetKBTimeout() }

// SetKeyboardTimeout sets that timeout (verified by read-back).
func (a *API) SetKeyboardTimeout(on bool) (bool, error) { return a.s.HW.SetKBTimeout(on) }

// GPUMode is the GPU (MUX) mode: optimus, nvidia or auto.
func (a *API) GPUMode() (string, error) { return a.s.HW.GetGPUMode() }

// SetGPUMode sets the GPU mode; it takes effect after a reboot.
func (a *API) SetGPUMode(mode string) (string, error) { return a.s.HW.SetGPUMode(mode) }
