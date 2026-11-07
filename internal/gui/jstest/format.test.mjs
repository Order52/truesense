import test from "node:test";
import assert from "node:assert/strict";
import { clamp, fmtTemp, fmtRpm, fmtPct, fmtWatt, fmtSigned, fmtDuration, describeLook, describeProfile, validName, capitalize, withSource } from "../web/js/format.js";

test("clamp keeps a value in range", () => {
  assert.equal(clamp(5, 0, 3), 3);
  assert.equal(clamp(-1, 0, 3), 0);
  assert.equal(clamp(2, 0, 3), 2);
});

test("readings format with units and show a dash for missing values", () => {
  assert.equal(fmtTemp(54.4), "54 °C");
  assert.equal(fmtTemp(null), "–");
  assert.equal(fmtTemp(undefined), "–");
  assert.equal(fmtTemp(NaN), "–");
  assert.equal(fmtRpm(2150), "2150 RPM");
  assert.equal(fmtPct(12.6), "13 %");
  assert.equal(fmtWatt(64.96), "65 W");
  assert.equal(fmtWatt(4.25), "4.3 W");
  assert.equal(fmtWatt(null), "–");
});

test("signed values use a real minus sign", () => {
  assert.equal(fmtSigned(120, "MHz"), "+120 MHz");
  assert.equal(fmtSigned(-50, "mV"), "−50 mV");
  assert.equal(fmtSigned(0, "mV"), "0 mV");
});

test("durations pick a readable unit", () => {
  assert.equal(fmtDuration(45), "45 s");
  assert.equal(fmtDuration(120), "2 min");
  assert.equal(fmtDuration(30 * 60), "30 min");
  assert.equal(fmtDuration(2 * 3600), "2 h");
  assert.equal(fmtDuration(-5), "0 s");
});

test("capitalize and source labels", () => {
  assert.equal(capitalize("turbo"), "Turbo");
  assert.equal(capitalize(""), "");
  assert.equal(withSource("gaming", "yours"), "gaming  (yours)");
  assert.equal(withSource("gaming", ""), "gaming");
});

test("names follow the command line rule", () => {
  for (const ok of ["a", "my-gaming", "x.y_z", "A".repeat(40)]) assert.ok(validName(ok), ok);
  for (const bad of ["", "a b", "x/y", "ü", "A".repeat(41), null]) assert.ok(!validName(bad), String(bad));
});

test("describeLook matches the command line wording", () => {
  assert.equal(
    describeLook({ keyboard: { mode: "perkey", brightness: 12 }, rear: { mode: "breathing", brightness: 100 } }),
    "keyboard perkey brightness 12; rear breathing brightness 100",
  );
  assert.equal(describeLook({ keyboard: { mode: "effect", name: "wave", brightness: 30, speed: 5 }, rear: null }), "keyboard wave brightness 30 speed 5; rear unchanged");
  assert.equal(describeLook({}), "keyboard unchanged; rear unchanged");
});

test("describeProfile lists one line per area", () => {
  assert.deepEqual(describeProfile({ power: { mode: "turbo" }, fan: { mode: "smart" }, display: { overdrive: true } }), [
    "Power: Turbo mode (held for a while)",
    "Fans: smart curve",
    "Screen: overdrive on",
  ]);
  assert.deepEqual(describeProfile({}), ["(nothing)"]);
  assert.deepEqual(describeProfile({ fan: { mode: "manual", cpu: 40, gpu: 50 }, battery: { limit: true, usb_charge: 10 }, boot: { sound: false }, rear: { on: false } }), [
    "Fans: manual 40 % / 50 %",
    "Rear light: off",
    "Battery: 80 % limit on, USB charging while off 10",
    "Boot sound off",
  ]);
});
