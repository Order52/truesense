package tui

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// The tests below are the ports of tests/test_tui.py, case by case, against the fake laptop.

func TestEveryPageDrawsAtEverySizeWithoutAnError(t *testing.T) {
	for _, sz := range [][2]int{{140, 50}, {100, 30}, {80, 24}, {60, 20}, {44, 12}} {
		h := newHarness(t, sz[0], sz[1])
		for i := range pageCount {
			h.app.Goto(i)
			txt := h.text()
			if !strings.Contains(txt, "TrueSense") {
				t.Errorf("%v page %d: no title", sz, i)
			}
			if widest(txt) > sz[0] {
				t.Errorf("%v page %d: a line is wider than the screen", sz, i)
			}
		}
	}
}

func TestATerminalThatIsTooSmallSaysSo(t *testing.T) {
	h := newHarness(t, 30, 8)
	txt := h.text()
	if !strings.Contains(txt, "44x12") || !strings.Contains(txt, "30x8") {
		t.Fatalf("got %q", txt)
	}
}

func TestTheDashboardShowsTheReadingsAndWorksWithoutTheService(t *testing.T) {
	h := newHarness(t, 100, 30)
	txt := h.text()
	for _, w := range []string{"Power mode", "Temperatures", "Fans", "Load", "Power", "CPU", "GPU", "direct", "°C", "rpm"} {
		if !strings.Contains(txt, w) {
			t.Errorf("missing %q in\n%s", w, txt)
		}
	}
}

func TestTheDashboardShowsTheServiceWhenItRuns(t *testing.T) {
	h := newHarness(t, 100, 30, func(b *fakeBackend) { b.service = true })
	txt := h.text()
	for _, w := range []string{"service", "quiet desktop", "native · profile balanced", "GPU power 9 W", "on charger"} {
		if !strings.Contains(txt, w) {
			t.Errorf("missing %q in\n%s", w, txt)
		}
	}
}

func TestNarrowTerminalsStackThePanelsAndKeepTheTabsUsable(t *testing.T) {
	h := newHarness(t, 70, 34)
	txt := h.text()
	for _, w := range []string{"Temperatures", "Power ", " 1 Dashboard "} {
		if !strings.Contains(txt, w) {
			t.Errorf("missing %q", w)
		}
	}
}

func TestPlainASCIIModeHasNoBoxOrArrowCharacters(t *testing.T) {
	h := newHarnessWith(t, 100, 30, newFake(), false)
	for i := range pageCount {
		h.app.Goto(i)
		for _, c := range h.text() {
			if c >= 0x2190 && c <= 0x2BFF {
				t.Fatalf("page %d draws %q in ASCII mode", i, c)
			}
		}
	}
}

func TestLongPagesScrollToKeepTheSelectionVisible(t *testing.T) {
	h := newHarness(t, 100, 16)
	h.app.Goto(3)
	h.keys("jjjjjjjjj")
	if txt := h.text(); !strings.Contains(txt, "▸") || h.app.page().scroll == 0 {
		t.Fatalf("scroll %d, text\n%s", h.app.page().scroll, txt)
	}
}

func TestWhatYouDidIsListedOnTheDashboard(t *testing.T) {
	h := newHarness(t, 100, 34)
	if !strings.Contains(h.text(), "Nothing yet") {
		t.Fatal("empty list not announced")
	}
	h.keys("l")
	h.elapse()
	txt := h.text()
	if !strings.Contains(txt, "What you did") || !strings.Contains(strings.SplitN(txt, "What you did", 2)[1], "Power mode") {
		t.Fatalf("change not listed:\n%s", txt)
	}
}

func TestTheHelpDialogOpensAndClosesOnAnyKey(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.keys("?")
	if !strings.Contains(h.text(), "Keys") {
		t.Fatal("help did not open")
	}
	h.keys("x")
	if strings.Contains(h.text(), "Keys") {
		t.Fatal("help did not close")
	}
}

func TestTabDigitsAndShiftTabChangePage(t *testing.T) {
	h := newHarness(t, 100, 30)
	for _, step := range []struct {
		k    Key
		want int
	}{{KeyTab, 1}, {'5', 4}, {KeyBacktab, 3}, {'1', 0}, {KeyBacktab, 6}} {
		h.app.Key(step.k)
		if h.app.pageI != step.want {
			t.Fatalf("after %v on page %d, want %d", step.k, h.app.pageI, step.want)
		}
	}
}

func TestOnlyQQuitsEscapeClosesADialogOrUndoesAChange(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.keys("q")
	if !h.app.quit {
		t.Fatal("q must quit")
	}
	h = newHarness(t, 100, 30)
	h.app.Key(KeyEsc)
	if h.app.quit {
		t.Fatal("a stray Esc must not quit")
	}
	h.app.confirm("sure?", func() {}, nil)
	h.app.Key(KeyEsc)
	if h.app.modal != nil || h.app.quit {
		t.Fatal("Esc closes the question and does not quit")
	}
}

func TestAChangeIsShownAtOnceWrittenAfterAPauseAndEnterWritesItNow(t *testing.T) {
	h := newHarness(t, 100, 30)
	row := h.app.page().rows[0] // the power mode; right = next option
	h.keys("l")
	if h.b.mode != "eco" {
		t.Fatal("not written yet: the pause lets a run of key presses become one write")
	}
	if row.Value() == "eco" || row.state != StatePending {
		t.Fatalf("but shown at once: value %v state %v", row.Value(), row.state)
	}
	h.app.Key(KeyEnter)
	if h.b.mode == "eco" || !strings.Contains(h.app.msg.text, "Power mode") || row.state != StateIdle {
		t.Fatalf("Enter did not write: mode %s msg %q state %v", h.b.mode, h.app.msg.text, row.state)
	}
	before := h.b.mode
	h.keys("lh")
	h.elapse()
	if h.b.mode != before {
		t.Fatal("a change taken back by the next key press is still written")
	}
}

func TestPowerModeIsHeldThirtyMinutes(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.keys("l")
	h.app.Key(KeyEnter)
	if h.b.hold.Minutes() != 30 {
		t.Fatalf("hold %v", h.b.hold)
	}
}

func TestMStepsThroughThePowerModesAndQuickPressesAreOneWrite(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.keys("mmm")
	if h.b.count("SetPowerMode") != 0 {
		t.Fatal("written before the pause")
	}
	h.elapse()
	calls := h.b.callList()
	var writes []string
	for _, c := range calls {
		if strings.HasPrefix(c, "SetPowerMode") {
			writes = append(writes, c)
		}
	}
	if len(writes) != 1 || writes[0] != "SetPowerMode "+modeOrder[3] { // eco -> quiet -> balanced -> performance
		t.Fatalf("three presses, one write, three steps: %v", writes)
	}
}

func TestEscapeTakesBackAChangeThatWasNotSent(t *testing.T) {
	h := newHarness(t, 100, 30)
	row := h.app.page().rows[0]
	h.keys("ll")
	h.app.Key(KeyEsc)
	h.elapse()
	if h.b.mode != "eco" || row.Value() != "eco" || row.state != StateIdle || len(h.app.pending) != 0 {
		t.Fatalf("mode %s value %v state %v pending %d", h.b.mode, row.Value(), row.state, len(h.app.pending))
	}
}

func TestAToggleFlipsAtOnceAndReadsBack(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(2)
	h.sel("Battery limit (80 %)")
	h.keys(" ")
	if !h.b.limit || !strings.Contains(h.app.msg.text, "Battery limit") {
		t.Fatalf("limit %v msg %q", h.b.limit, h.app.msg.text)
	}
}

func TestTheFanSliderWritesOnceWhenTheArrowKeysPause(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	h.keys("j") // Mode -> CPU fan
	h.keys("lll")
	if len(h.app.pending) != 1 || h.app.pending[0] != h.app.page().rows[h.app.page().sel] {
		t.Fatal("queued, not written on every key")
	}
	h.elapse()
	if n := h.b.count("SetFan"); n != 1 || h.b.duty[0] != 45 {
		t.Fatalf("%d fan writes, duty %v", n, h.b.duty)
	}
}

func TestABrightnessOfZeroTurnsTheLightOffItIsNotAFanStop(t *testing.T) {
	for _, tc := range []struct {
		name string
		nth  int
		read func(*fakeBackend) int
	}{
		{"rear", 1, func(b *fakeBackend) int { return b.rear.Brightness }},
		{"kb", 0, func(b *fakeBackend) int { return b.kbNow }},
	} {
		h := newHarness(t, 100, 30, func(b *fakeBackend) {
			b.rear = RearLook{Mode: "static", Color: "ffffff", Speed: 3, Brightness: 60, Direction: 1}
		})
		h.app.Goto(3)
		h.sel("Brightness", tc.nth)
		h.keys("hhhhhhhhhhhh")
		if h.app.modal != nil {
			t.Fatalf("%s: zero is a normal value for a light", tc.name)
		}
		h.elapse()
		if got := tc.read(h.b); got != 0 {
			t.Errorf("%s: brightness %d, want 0", tc.name, got)
		}
		text, st := msgOf(h.app)
		if strings.Contains(strings.ToLower(text), "fan") || st != StGood {
			t.Errorf("%s: message %q (%s)", tc.name, text, st)
		}
	}
}

func TestChoosingAnEffectNeverLeavesADarkLightDark(t *testing.T) {
	h := newHarness(t, 100, 30, func(b *fakeBackend) {
		b.rear = RearLook{Mode: "static", Color: "ffffff", Speed: 3, Brightness: 60, Direction: 1}
	})
	h.app.Goto(3)
	h.sel("Brightness", 1)
	h.keys("hhhhhhhhhhhh")
	h.elapse()
	if h.b.rear.Brightness != 0 {
		t.Fatalf("brightness %d", h.b.rear.Brightness)
	}
	h.app.edit(h.sel("Effect", 1), "breathing", true)
	if h.b.rear.Mode != "breathing" || h.b.rear.Brightness < 40 {
		t.Fatalf("an effect picked while the bar is off turns it on at a usable brightness: %+v", h.b.rear)
	}
}

func TestAKeyboardEffectPickedWhileDarkComesBackAtTheFloor(t *testing.T) {
	h := newHarness(t, 100, 30, func(b *fakeBackend) { b.kbNow = 0 })
	h.app.Goto(3)
	h.app.edit(h.sel("Effect", 0), "wave", true)
	if h.b.kb.Name != "wave" || h.b.kb.Brightness < lightFloor["kb"] {
		t.Fatalf("%+v", h.b.kb)
	}
	h2 := newHarness(t, 100, 30, func(b *fakeBackend) { b.kbNow = 0 })
	h2.app.Goto(3)
	h2.app.edit(h2.sel("Effect", 0), "off", true)
	if h2.b.kb.Name != "off" {
		t.Fatalf("choosing off must stay off: %+v", h2.b.kb)
	}
}

func TestEnterOnASliderThatWasNotChangedWritesNothing(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	h.keys("j")
	before := len(h.b.writeCalls())
	h.app.Key(KeyEnter)
	h.keys(" ")
	if h.app.modal != nil || len(h.b.writeCalls()) != before || len(h.app.pending) != 0 {
		t.Fatalf("writes %v pending %d", h.b.writeCalls(), len(h.app.pending))
	}
}

func TestASliderWithAnUnknownReadingStartsInTheMiddleNeverAtOff(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	row := h.sel("CPU fan")
	h.setFan(nil)
	if _, ok := row.Index(); ok {
		t.Fatal("index known with no reading")
	}
	h.keys("h")
	if row.state != StatePending || row.Value() == 0 || h.app.modal != nil {
		t.Fatalf("state %v value %v modal %v", row.state, row.Value(), h.app.modal)
	}
	h.app.undoAll()
	h.keys("l")
	if v, _ := asInt(row.Value()); v < 20 {
		t.Fatalf("value %d", v)
	}
}

func TestAFanThatRunsAtTenPercentIsNotShownAsOff(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	row := h.sel("CPU fan")
	h.setFan(&FanState{Mode: FirmwareFanManual, CPUDuty: 10, GPUDuty: 40})
	if i, _ := row.Index(); row.Values[i] != 20 {
		t.Fatalf("10 %% shows as %d", row.Values[i])
	}
	h.setFan(&FanState{Mode: FirmwareFanManual, CPUDuty: 0, GPUDuty: 40})
	if i, _ := row.Index(); row.Values[i] != 0 {
		t.Fatalf("0 shows as %d", row.Values[i])
	}
}

func TestStoppingAFanAsksDefaultsToNoAndIsRefusedWithoutTheService(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	h.keys("j")
	h.keys("hhhhhhhh") // down to the bottom stop
	if h.app.modal != nil || h.b.duty[0] == 0 {
		t.Fatal("moving there does not ask or write")
	}
	h.app.Key(KeyEnter)
	if h.app.modal == nil || h.app.modal.kind != modalConfirm || h.app.modal.focus != 1 {
		t.Fatal("a question with No marked was expected")
	}
	before := h.b.duty
	h.app.Key(KeyEnter)
	if h.app.modal != nil || h.b.duty != before || !strings.Contains(h.app.msg.text, "Cancelled") {
		t.Fatal("Enter on the default answer cancels")
	}
	if v := h.sel("CPU fan").Value(); v != h.b.duty[0] {
		t.Fatalf("the slider shows %v, the fan runs at %d", v, h.b.duty[0])
	}
	h.keys("hhhhhhhh")
	h.app.Key(KeyEnter)
	h.app.Key('y')
	text, st := msgOf(h.app)
	if h.b.duty != before || st != StBad || !strings.Contains(text, "service") {
		t.Fatalf("a stop needs the service: duty %v msg %q (%s)", h.b.duty, text, st)
	}
}

func TestTheStopQuestionCanBeAnsweredWithTheArrowsAndEnterButEnterAloneNeverSaysYes(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	row := h.sel("Mode")
	h.app.edit(row, "off", true)
	if h.app.modal == nil || h.app.modal.kind != modalConfirm || h.app.modal.focus != 1 {
		t.Fatal("no question")
	}
	h.keys("h")
	if h.app.modal.focus != 0 {
		t.Fatal("h moves to Yes")
	}
	h.keys("l")
	if h.app.modal.focus != 1 {
		t.Fatal("l moves to No")
	}
	h.app.Key(KeyEnter)
	if h.app.modal != nil || row.state != StateIdle || h.b.count("SetFan") != 0 {
		t.Fatal("Enter on No must not write")
	}
	h.app.edit(row, "off", true)
	h.keys("h")
	h.app.Key(KeyEnter)
	if _, st := msgOf(h.app); st != StBad || h.b.count("SetFan") != 1 {
		t.Fatal("Yes was chosen: the write was tried (and refused: no service)")
	}
}

func TestAQuestionThatIsNotAnsweredIsANoWhenYouQuit(t *testing.T) {
	h := newHarness(t, 100, 30, func(b *fakeBackend) { b.service = true })
	h.app.Goto(1)
	h.keys("j")
	h.keys("hhhhhhhh")
	h.app.Flush()
	if h.b.count("SetFan") != 0 || len(h.app.pending) != 0 {
		t.Fatalf("a stop was written on quit: %v", h.b.callList())
	}
	// And a question that is open (asked, not answered) is dropped too.
	h = newHarness(t, 100, 30, func(b *fakeBackend) { b.service = true })
	h.app.Goto(1)
	h.app.edit(h.sel("Mode"), "off", true)
	h.app.Flush()
	if h.b.count("SetFan") != 0 {
		t.Fatal("an open question is a No")
	}
}

func TestAHintBelongsToItsRowAndIsGoneWhenYouMove(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	h.keys("j")
	h.app.Key(KeyEnter)
	if !strings.Contains(h.app.msg.text, "Use") || !strings.Contains(h.text(), "Use") {
		t.Fatal("hint not shown")
	}
	h.keys("j")
	if strings.Contains(h.text(), "applied by itself") {
		t.Fatal("the hint does not follow you to another row")
	}
	h.keys("k")
	h.app.Key(KeyEnter)
	h.app.Key(KeyTab)
	if strings.Contains(h.text(), "applied by itself") {
		t.Fatal("nor to another page")
	}
}

func TestChoosingManualOnTheFanModeRowWorksAndKeepsBothFansRunning(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	h.app.edit(h.sel("Mode"), "manual", true)
	if text, st := msgOf(h.app); st != StGood || h.b.fanMode != FirmwareFanManual || min(h.b.duty[0], h.b.duty[1]) < 20 {
		t.Fatalf("%q duty %v", text, h.b.duty)
	}
}

func TestMovingOneFanSliderNeverStopsTheOtherFan(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	h.setFan(&FanState{Mode: FirmwareFanAuto, CPUDuty: 0, GPUDuty: 0}) // an odd reading must not become a stop
	h.app.edit(h.sel("CPU fan"), 60, true)
	if h.b.duty[0] != 60 || h.b.duty[1] < 20 {
		t.Fatalf("duty %v", h.b.duty)
	}
}

func TestACustomColourIsTypedInADialogAndChecked(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(3)
	h.sel("Custom colour")
	h.app.Key(KeyEnter)
	if h.app.modal == nil || h.app.modal.kind != modalPrompt {
		t.Fatal("no prompt")
	}
	h.keys("ff8800")
	h.app.Key(KeyEnter)
	h.elapse()
	if h.b.kb.Color != "ff8800" {
		t.Fatalf("colour %q", h.b.kb.Color)
	}
	h.app.Key(KeyEnter)
	h.keys("nothex")
	h.app.Key(KeyEnter)
	if _, st := msgOf(h.app); h.b.kb.Color != "ff8800" || st != StBad {
		t.Fatal("a bad colour is refused")
	}
}

func TestAValueThatCouldNotBeWrittenIsNotLeftOnScreen(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.b.failOn("SetOverdrive", errors.New("firmware said no"))
	h.app.Goto(2)
	row := h.sel("Panel overdrive")
	was := row.Value()
	h.keys(" ")
	text, st := msgOf(h.app)
	if st != StBad || !strings.Contains(text, "firmware said no") || row.Value() != was || row.state != StateIdle {
		t.Fatalf("msg %q (%s) value %v was %v state %v", text, st, row.Value(), was, row.state)
	}
}

func TestAFailedLightWriteForgetsTheRememberedColour(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.b.failOn("SetKeyboard", errors.New("no"))
	h.app.Goto(3)
	h.app.edit(h.sel("Colour"), "ff0000", true)
	if _, st := msgOf(h.app); st != StBad {
		t.Fatal("failure not reported")
	}
	if _, ok := h.app.kb["color"]; ok {
		t.Fatal("the colour that failed is still remembered")
	}
}

func TestAButtonMarksItsOwnRowBusyAndNotTheRowYouChangedLast(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(2)
	other := h.sel("Boot sound")
	h.keys(" ")
	if other.state != StateIdle {
		t.Fatal("the toggle is still busy")
	}
	h.app.Goto(5)
	var btn *Row
	for i, r := range h.app.page().rows {
		if r.Kind == KindButton {
			btn, h.app.page().sel = r, i
			break
		}
	}
	var seen []RowState
	btn.Press = func() {
		h.app.run("Profile y", func() (any, error) { seen = append(seen, btn.state); return nil, nil }, runOpts{})
	}
	h.app.Key(KeyEnter)
	if len(seen) != 1 || seen[0] != StateWriting || btn.state != StateIdle || other.state != StateIdle {
		t.Fatalf("seen %v button %v other %v", seen, btn.state, other.state)
	}
}

func TestAButtonThatIsStillRunningIsNotStartedTwice(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(5)
	var btn *Row
	for i, r := range h.app.page().rows {
		if r.Kind == KindButton {
			btn, h.app.page().sel = r, i
			break
		}
	}
	calls := 0
	btn.Press = func() { calls++ }
	btn.state = StateWriting
	h.app.Key(KeyEnter)
	if calls != 0 || !strings.Contains(h.app.msg.text, "Still working") {
		t.Fatalf("calls %d msg %q", calls, h.app.msg.text)
	}
}

func TestAFailedActionIsReportedNotRaised(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(5)
	for i, r := range h.app.page().rows {
		if r.Kind == KindButton {
			h.app.page().sel = i
			r.Press = func() {
				h.app.run("Profile x", func() (any, error) { return nil, errors.New("boom") }, runOpts{})
			}
			break
		}
	}
	h.app.Key(KeyEnter)
	if text, st := msgOf(h.app); st != StBad || !strings.Contains(text, "boom") {
		t.Fatalf("%q (%s)", text, st)
	}
}

func TestQuittingSendsAChangeMadeAMomentAgo(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.keys("l")
	h.app.Flush()
	if h.b.mode == "eco" {
		t.Fatal("the change was lost")
	}
}

func TestTheBottomLineShowsTheKeysOfTheSelectedRow(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	h.sel("CPU fan")
	if !strings.Contains(h.lastLine(), "adjust") {
		t.Fatal("slider keys")
	}
	h.app.Goto(3)
	h.sel("Effect")
	if !strings.Contains(h.lastLine(), "choose") {
		t.Fatal("choice keys")
	}
	h.app.Goto(2)
	h.sel("Boot sound")
	if !strings.Contains(h.lastLine(), "switch") {
		t.Fatal("toggle keys")
	}
}

func TestEscAppearsInTheBottomLineOnlyWhileAChangeWaits(t *testing.T) {
	h := newHarness(t, 100, 30)
	if strings.Contains(h.lastLine(), "Esc") {
		t.Fatal("Esc listed with nothing to undo")
	}
	h.keys("l")
	if !strings.Contains(h.lastLine(), "Esc undo") {
		t.Fatalf("%q", h.lastLine())
	}
}

func TestThePendingMarkerIsDrawnLast(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.keys("l")
	line := strings.Split(h.text(), "\n")[2]
	r := []rune(line)
	if r[100-3] != '↵' {
		t.Fatalf("no marker at column %d of %q", 100-3, line)
	}
	h.app.Key(KeyEnter) // now writing -> settled in sync mode, marker gone
	if strings.ContainsRune(strings.Split(h.text(), "\n")[2], '↵') {
		t.Fatal("marker left after the write")
	}
}

func TestClickingYesOrNoInTheQuestionAnswersIt(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(2)
	row := h.sel("GPU mode")
	h.app.edit(row, "nvidia", true)
	txt := h.text()
	if !strings.Contains(txt, "Switch the GPU mode") || !strings.Contains(txt, "[ Yes ]") {
		t.Fatal("question not drawn")
	}
	if !h.click("[ No ]", -1) || h.app.modal != nil || h.b.gpu != "optimus" {
		t.Fatal("No must cancel")
	}
	h.app.edit(row, "nvidia", true)
	if !h.click("[ Yes ]", -1) || h.b.gpu != "nvidia" {
		t.Fatal("Yes must write")
	}
}

func TestDraggingASliderFollowsThePointerAndWritesWhenReleased(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(3)
	row := h.sel("Brightness", 0)
	ht := h.barHit(row)
	x0, bw, y := ht.x0, ht.extra, ht.y
	h.app.Mouse(x0, y, 0, true)
	if row.Value() != row.Values[0] {
		t.Fatalf("press at the left end gives %v", row.Value())
	}
	n := h.b.count("SetKeyboard")
	for x := x0; x < x0+bw; x += 3 {
		h.app.MouseMove(x, y)
	}
	h.app.MouseMove(x0+bw+10, y)
	if row.Value() != row.Values[len(row.Values)-1] {
		t.Fatalf("past the end is the end: %v", row.Value())
	}
	if h.b.count("SetKeyboard") != n {
		t.Fatal("a drag must not write on every step")
	}
	h.app.MouseRelease()
	if got := h.b.count("SetKeyboard") - n; got != 1 || h.b.kbNow != row.Values[len(row.Values)-1] || h.app.drag != nil {
		t.Fatalf("writes after release %d, brightness %d", got, h.b.kbNow)
	}
}

func TestAKeyPressEndsADrag(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(3)
	ht := h.barHit(h.sel("Brightness", 0))
	h.app.Mouse(ht.x0, ht.y, 0, true)
	if h.app.drag == nil {
		t.Fatal("no drag")
	}
	h.keys("j")
	if h.app.drag != nil {
		t.Fatal("drag survived a key")
	}
}

func TestClickingATabChangesPage(t *testing.T) {
	h := newHarness(t, 100, 30)
	if !h.click("Fans", 1) || h.app.pageI != 1 {
		t.Fatalf("page %d", h.app.pageI)
	}
}

func TestClickingAnOptionSelectsIt(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(2)
	if !h.click("Turbo", -1) || h.b.mode != "turbo" {
		t.Fatalf("mode %s", h.b.mode)
	}
}

func TestClickingAToggleFlipsIt(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(2)
	row := h.sel("Boot sound")
	h.app.Draw()
	for _, ht := range h.app.hits {
		if ht.row == row && ht.zone == zoneToggle {
			h.app.Mouse(ht.x0+1, ht.y, 0, true)
			break
		}
	}
	if !h.b.boot {
		t.Fatal("not flipped")
	}
}

func TestClickingASliderSetsThatPositionAndTheWheelMovesTheSelection(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(1)
	h.app.Draw()
	var bar hit
	for _, ht := range h.app.hits {
		if ht.zone == zoneBar {
			bar = ht
			break
		}
	}
	h.app.Mouse(bar.x1-1, bar.y, 0, true)
	row := h.app.page().rows[h.app.page().sel]
	if i, _ := row.Index(); i != len(row.Values)-1 {
		t.Fatalf("index %d", i)
	}
	sel := h.app.page().sel
	h.app.Mouse(5, 5, -1, false)
	if h.app.page().sel == sel {
		t.Fatal("the wheel did not move the selection")
	}
}

func TestClicksWhileADialogIsOpenDoNotReachThePage(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(2)
	h.app.confirm("x", func() {}, nil)
	h.click("Turbo", -1)
	if h.b.mode != "eco" {
		t.Fatal("the click reached the page")
	}
}

func TestEveryPageHasItsControls(t *testing.T) {
	want := map[int][]string{1: {"Fan control", "CPU fan", "GPU fan", "Curve"}, 2: {"Battery limit", "GPU mode", "USB charging", "Smart profile"},
		3: {"Keyboard", "Light bar", "Brightness", "Direction"}, 4: {"Acer DTS sound", "Microphone", "Fix sound", "DTS sound mode"}, 5: {"Profiles", "Looks"}, 6: {"Health check", "Version"}}
	h := newHarness(t, 110, 60)
	for i, words := range want {
		h.app.Goto(i)
		txt := h.text()
		for _, w := range words {
			if !strings.Contains(txt, w) {
				t.Errorf("page %d lacks %q", i, w)
			}
		}
	}
}

func TestDoctorResultsAreHiddenUntilTheCheckHasRunAndThenColoured(t *testing.T) {
	h := newHarness(t, 110, 60)
	h.app.Goto(6)
	if txt := h.text(); !strings.Contains(txt, "not run yet") || strings.Contains(txt, "Kernel headers") {
		t.Fatal("results shown before the check")
	}
	h.app.data.Set("doctor", []DoctorResult{{"Kernel", DoctorOK, "fine", ""}, {"Audio", DoctorWarn, "no output", "truesense audio repair"}})
	txt := h.text()
	if !strings.Contains(txt, "fine") || !strings.Contains(txt, "truesense audio repair") || strings.Contains(txt, "not run yet") {
		t.Fatalf("results not shown:\n%s", txt)
	}
	for y, line := range strings.Split(txt, "\n") {
		if strings.Contains(line, "fine") {
			col := utf8.RuneCountInString(line[:strings.Index(line, "OK")])
			if st := h.cv.StyleAt(y, col); st != StGood {
				t.Fatalf("OK is drawn %s", st)
			}
			return
		}
	}
	t.Fatal("no line with fine")
}

func TestRunningTheDoctorFillsThePage(t *testing.T) {
	h := newHarness(t, 110, 60, func(b *fakeBackend) {
		b.doctor = []DoctorResult{{"Kernel", DoctorOK, "fine", ""}, {"Audio", DoctorFail, "silent", "repair it"}}
	})
	h.app.Goto(6)
	h.sel("Run the check")
	h.app.Key(KeyEnter)
	txt := h.text()
	if !strings.Contains(txt, "fine") || !strings.Contains(txt, "-> repair it") || h.app.msg.text != "Check finished" {
		t.Fatalf("msg %q\n%s", h.app.msg.text, txt)
	}
}

func TestOpenTheWindowRunsTheBackend(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(6)
	h.sel("Open the window")
	h.app.Key(KeyEnter)
	if h.b.window != 1 {
		t.Fatal("window not opened")
	}
}

func TestTheAudioPageShowsWhatTheSoundSystemReports(t *testing.T) {
	h := newHarness(t, 110, 40, func(b *fakeBackend) {
		b.devices = []DTSDevice{{Label: "Headset PC38X", Profile: "Music", Current: true}}
	})
	h.app.Goto(4)
	if txt := h.text(); !strings.Contains(txt, "Headset PC38X: Music") || !strings.Contains(txt, "output present") {
		t.Fatalf("\n%s", txt)
	}
}

func TestTheSoundRepairReportsWhatItDid(t *testing.T) {
	for _, tc := range []struct {
		res  RepairResult
		want string
	}{
		{RepairResult{Fixed: true, Changed: true, Steps: []string{"restarted WirePlumber"}}, "Sound output repaired: restarted WirePlumber"},
		{RepairResult{Fixed: true}, "The sound output is there: nothing to repair"},
		{RepairResult{Steps: []string{"no card"}}, "Could not repair: no card"},
	} {
		h := newHarness(t, 100, 40, func(b *fakeBackend) { b.repair = tc.res })
		h.app.Goto(4)
		h.sel("No sound at all?")
		h.app.Key(KeyEnter)
		if h.app.msg.text != tc.want {
			t.Errorf("got %q want %q", h.app.msg.text, tc.want)
		}
	}
}

func TestTheProfileListMatchesTheBackendsProfiles(t *testing.T) {
	h := newHarness(t, 110, 40)
	h.app.Goto(5)
	txt := h.text()
	for _, n := range h.b.ProfileNames() {
		if !strings.Contains(txt, n) {
			t.Errorf("profile %s missing", n)
		}
	}
	h.sel("gaming")
	h.app.Key(KeyEnter)
	if h.app.msg.text != "Profile gaming applied" || h.b.count("ApplyProfile gaming") != 1 {
		t.Fatalf("%q", h.app.msg.text)
	}
}

func TestAMissingDeviceIsReportedNotFatal(t *testing.T) {
	b := newFake()
	b.failOn("Sensors", errors.New("no device"))
	h := newHarnessWith(t, 100, 30, b, true)
	if txt := h.text(); !strings.Contains(txt, "No device: no device") {
		t.Fatalf("\n%s", txt)
	}
}

func TestHistoryFeedsTheSparklines(t *testing.T) {
	h := newHarness(t, 100, 30)
	for range 10 {
		h.app.Data().Fast(h.app.ctx)
	}
	if v := h.app.Data().view(); len(v.hist[histTCPU]) < 10 {
		t.Fatalf("%d points", len(v.hist[histTCPU]))
	}
}

func TestStaticMenuEntryOpensTheTUIInATerminalAndIsInstalledEverywhere(t *testing.T) {
	read := func(p string) string {
		b, err := readFile("../../" + p)
		if err != nil {
			t.Skipf("packaging files not available: %v", err)
		}
		return b
	}
	d := map[string]string{}
	for _, l := range strings.Split(read("packaging/truesense-tui.desktop"), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			d[k] = v
		}
	}
	if d["Exec"] != "@PREFIX@/bin/truesense tui" || d["Terminal"] != "true" || d["Type"] != "Application" || d["Name"] == "" || d["Icon"] == "" {
		t.Fatalf("%v", d)
	}
	for _, f := range []string{"tools/install.sh", "tools/uninstall.sh", "packaging/arch/PKGBUILD", "packaging/aur/PKGBUILD.template"} {
		if !strings.Contains(read(f), "truesense-tui.desktop") {
			t.Errorf("%s does not mention the menu entry", f)
		}
	}
}

func TestAPanickingBackendCallIsReportedAndDoesNotLeaveTheRowBusy(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(2)
	row := h.sel("Boot sound")
	h.app.acting = row
	h.app.run("Boot sound", func() (any, error) { panic("kaboom") }, runOpts{row: row})
	if text, st := msgOf(h.app); st != StBad || !strings.Contains(text, "kaboom") || row.state != StateIdle {
		t.Fatalf("%q (%s) state %v", text, st, row.state)
	}
}

func TestAClickOnAnOptionThatNoLongerExistsIsIgnored(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(3)
	h.sel("Saved lighting")
	h.b.mu.Lock()
	h.b.saved = []string{"a", "b", "c"}
	h.b.mu.Unlock()
	h.app.Refresh()
	h.app.Draw() // the frame has three options
	h.b.mu.Lock()
	h.b.saved = nil
	h.b.mu.Unlock()
	h.app.Refresh()
	for _, ht := range h.app.hits {
		if ht.zone == zoneOpt && ht.row.Label == "Saved lighting" {
			h.app.Mouse(ht.x0, ht.y, 0, true) // must not panic
		}
	}
}

func TestTheKeyboardShownIsTheKeyboardsRealBrightnessAndOffKeepsTheOtherSettings(t *testing.T) {
	h := newHarness(t, 100, 30, func(b *fakeBackend) {
		b.kb, b.kbKnown, b.kbNow = KeyboardLook{Name: "wave", Color: "ff0000", Speed: 7, Brightness: 20, Direction: 2}, true, 35 // Fn+F12 raised it
	})
	h.app.Goto(3)
	if s := h.app.kbs(); s.name != "wave" || s.color != "ff0000" || s.speed != 7 || s.direction != 2 || s.brightness != 35 {
		t.Fatalf("%+v", s)
	}
	h.b.mu.Lock()
	h.b.kb, h.b.kbNow = KeyboardLook{Name: "off"}, 0
	h.b.mu.Unlock()
	h.app.Refresh()
	if s := h.app.kbs(); s.name != "off" || s.speed != 5 || s.brightness != 0 {
		t.Fatalf("an off light shows as off with the defaults for the rest: %+v", s)
	}
}

func TestTheLightBarKeepsItsPartColoursUntilAColourIsPickedHere(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(3)
	h.app.edit(h.sel("Effect", 1), "wave", true)
	if h.b.zones {
		t.Fatal("part colours only exist in static mode")
	}
	h.app.edit(h.sel("Effect", 1), "static", true)
	if !h.b.zones {
		t.Fatal("kept for static without a colour pick")
	}
	h.app.edit(h.sel("Colour", 1), "ff0000", true)
	if h.b.zones || h.b.rear.Color != "ff0000" {
		t.Fatalf("a picked colour replaces the parts: %+v zones=%v", h.b.rear, h.b.zones)
	}
}

func TestSelectionSkipsRowsThatCannotBeUsedAndWraps(t *testing.T) {
	h := newHarness(t, 100, 30) // no service: the automatic mode row is not selectable
	if h.app.page().sel != 0 {
		t.Fatalf("first selectable row is %d", h.app.page().sel)
	}
	h.keys("j")
	if got := h.app.page().rows[h.app.page().sel].Label; got != "Fans" {
		t.Fatalf("down skipped to %q, want the Fans row", got)
	}
	h.keys("j")
	if h.app.page().sel != 0 {
		t.Fatal("down from the last row must wrap")
	}
	h.app.Key(KeyEnd)
	if h.app.page().sel != 2 {
		t.Fatalf("End: %d", h.app.page().sel)
	}
	h.app.Key(KeyHome)
	if h.app.page().sel != 0 {
		t.Fatalf("Home: %d", h.app.page().sel)
	}
	h.app.Goto(2)
	h.app.Key(KeyPageDown)
	h.app.Key(KeyPageUp)
	if !h.app.page().rows[h.app.page().sel].Selectable() {
		t.Fatal("paging landed on a row that cannot be used")
	}
}

func TestRKeyReadsEverythingAgainAndForgetsRememberedLightChanges(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Goto(3)
	h.app.kb["color"] = "123456"
	h.keys("r")
	if _, ok := h.app.kb["color"]; ok || h.app.msg.text != "Reading everything again…" {
		t.Fatalf("kb %v msg %q", h.app.kb, h.app.msg.text)
	}
}

func TestCtrlLAsksForAFullRepaint(t *testing.T) {
	h := newHarness(t, 100, 30)
	h.app.Key(KeyCtrlL)
	if !h.app.TakeRepaint() || h.app.TakeRepaint() {
		t.Fatal("repaint flag")
	}
}

func TestEveryPageIsReachableAndKeepsItsSelectionVisibleWhenResized(t *testing.T) {
	h := newHarness(t, 100, 30)
	for i := range pageCount {
		h.app.Goto(i)
		h.app.Key(KeyEnd)
		for _, sz := range [][2]int{{100, 30}, {44, 12}, {80, 14}} {
			h.cv.Resize(sz[0], sz[1])
			h.app.Draw()
			if !strings.Contains(h.cv.Text(), "▸") && h.app.page().rows[h.app.page().sel].Selectable() {
				t.Errorf("page %d at %v: the selected row is not visible", i, sz)
			}
		}
	}
}
