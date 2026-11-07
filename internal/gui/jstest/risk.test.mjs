import test from "node:test";
import assert from "node:assert/strict";
import { cpuRisk, gpuRisk, plRisk } from "../web/js/risk.js";

test("CPU undervolt zones", () => {
  assert.equal(cpuRisk(0, 0).level, "safe");
  assert.equal(cpuRisk(0, 0).why, "Stock voltage.");
  assert.equal(cpuRisk(-50, -40).why, "Inside the safe range.");
  assert.equal(cpuRisk(-60, -50).level, "safe");
  assert.equal(cpuRisk(-61, 0).level, "caution");
  assert.equal(cpuRisk(0, -51).level, "caution");
  assert.equal(cpuRisk(-90, -70).level, "caution");
  assert.equal(cpuRisk(-91, 0).level, "danger");
  assert.equal(cpuRisk(0, -71).level, "danger");
  assert.equal(cpuRisk(100, 0).level, "danger", "sign does not matter");
});

test("GPU undervolt zones", () => {
  assert.equal(gpuRisk(0, 0).why, "Stock.");
  assert.equal(gpuRisk(120, 2400).level, "safe");
  assert.equal(gpuRisk(151, 2400).level, "caution");
  assert.equal(gpuRisk(201, 2400).level, "danger");
  assert.equal(gpuRisk(100, 0).level, "caution");
  assert.match(gpuRisk(100, 0).why, /clock cap/);
  assert.equal(gpuRisk(-150, 0).level, "caution");
  assert.equal(gpuRisk(0, 1400).level, "caution");
  assert.equal(gpuRisk(0, 2000).level, "safe");
});

test("power limit zones", () => {
  assert.equal(plRisk(65, 80).level, "safe");
  assert.equal(plRisk(80, 100).level, "safe");
  assert.equal(plRisk(81, 100).level, "caution");
  assert.equal(plRisk(80, 101).level, "caution");
  assert.equal(plRisk(30, 80).level, "caution");
  assert.equal(plRisk(101, 110).level, "danger");
  assert.equal(plRisk(80, 116).level, "danger");
  const bad = plRisk(80, 60);
  assert.equal(bad.level, "danger");
  assert.ok(bad.invalid);
});
