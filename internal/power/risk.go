package power

import "fmt"

// Level is how risky a setting is.
type Level string

// The three risk levels.
const (
	Safe    Level = "safe"
	Caution Level = "caution"
	Danger  Level = "danger"
)

// This laptop's stock PL1/PL2 is 80 W: inside the safe range is fine, beyond it the cooling is
// the limit. Past the danger line the cooling cannot keep up at all.
const (
	safePL1Min = 35
	safePL1Max = 80
	safePL2Min = 35
	safePL2Max = 100
	dangerPL1  = 100
	dangerPL2  = 115
)

// Risk rates a PL1/PL2 pair (in watts) and explains the rating in words the user can read.
// The daemon refuses a Danger pair unless the caller says force.
func Risk(pl1, pl2 float64) (Level, string) {
	switch {
	case pl1 > dangerPL1 || pl2 > dangerPL2:
		return Danger, fmt.Sprintf("Beyond the danger line (%d W sustained / %d W burst): the cooling cannot keep up and the CPU will throttle hard or overheat.", dangerPL1, dangerPL2)
	case pl1 > safePL1Max || pl2 > safePL2Max:
		return Caution, fmt.Sprintf("Above stock (%d W sustained / %d W burst): hotter and louder, and it may throttle.", safePL1Max, safePL2Max)
	case pl1 < safePL1Min || pl2 < safePL2Min:
		return Caution, "Very low limits make the laptop feel slow."
	}
	return Safe, "Inside the safe range."
}
