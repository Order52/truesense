import test from "node:test";
import assert from "node:assert/strict";
import { interp, snapDuty, movePoint, addPoint, removePoint, nudge, validateSide, validateCurve, makeScale, linePoints, T0, T1 } from "../web/js/curve.js";

const truesenseCpu = [[52, 0], [56, 30], [62, 38], [68, 48], [74, 58], [80, 70], [85, 85], [89, 100]];
const truesenseGpu = [[48, 0], [54, 30], [60, 38], [65, 48], [70, 60], [74, 78], [78, 100]];

test("interp is piecewise linear and clamped", () => {
  const p = [[50, 0], [60, 40], [70, 100]];
  assert.equal(interp(p, 40), 0);
  assert.equal(interp(p, 55), 20);
  assert.equal(interp(p, 65), 70);
  assert.equal(interp(p, 90), 100);
  assert.equal(interp([[50, 30], [50, 80]], 50), 30);
});

test("snapDuty: firmware auto below 12, else 20..100 in steps of 5", () => {
  assert.equal(snapDuty(5), 0);
  assert.equal(snapDuty(11.9), 0);
  assert.equal(snapDuty(12), 20);
  assert.equal(snapDuty(33), 35);
  assert.equal(snapDuty(250), 100);
});

test("movePoint keeps order and a rising duty", () => {
  const p = [[52, 0], [60, 40], [70, 60], [80, 100]];
  const m = movePoint(p, 1, 75, 90); // cannot pass its neighbour in time or exceed its duty
  assert.deepEqual(m[1], [69, 60]);
  assert.deepEqual(movePoint(p, 1, 40, 0)[1], [53, 0]);
  assert.deepEqual(movePoint(p, 0, 10, 100)[0], [30, 40]);
  assert.deepEqual(p[1], [60, 40], "the input is not modified");
});

test("addPoint sits on the line, refuses crowding and a full curve", () => {
  const p = [[50, 0], [60, 40], [70, 100]];
  const a = addPoint(p, 65);
  assert.deepEqual(a.map((x) => x[0]), [50, 60, 65, 70]);
  assert.equal(a[2][1], 70);
  assert.equal(addPoint(p, 61), null);
  assert.equal(addPoint(Array.from({ length: 12 }, (_, i) => [40 + i * 4, 20 + i * 5]), 90), null);
});

test("removePoint keeps at least two", () => {
  assert.equal(removePoint([[50, 0], [60, 40]], 0), null);
  assert.deepEqual(removePoint([[50, 0], [60, 40], [70, 100]], 1), [[50, 0], [70, 100]]);
});

test("nudge steps temperature by one and duty to the next multiple of five, with the 0/20 jump", () => {
  const p = [[52, 0], [60, 40], [80, 100]];
  assert.deepEqual(nudge(p, 1, 1, 0)[1], [61, 40]);
  assert.deepEqual(nudge(p, 1, 0, 5)[1], [60, 45]);
  assert.deepEqual(nudge(p, 1, 0, -5)[1], [60, 35]);
  assert.deepEqual(nudge(p, 0, 0, 5)[0], [52, 20]);
  assert.deepEqual(nudge([[52, 20], [60, 40], [80, 100]], 0, 0, -5)[0], [52, 0]);
  const shipped = [[52, 0], [62, 38], [89, 100]];
  assert.equal(nudge(shipped, 1, 0, 5)[1][1], 40, "38 goes to 40, not 43");
  assert.equal(nudge(shipped, 1, 0, -5)[1][1], 35);
});

test("the shipped curves validate; broken ones say why", () => {
  assert.equal(validateCurve({ cpu: truesenseCpu, gpu: truesenseGpu }), null);
  assert.match(validateSide("cpu", [[52, 0]]), /2 to 12/);
  assert.match(validateSide("cpu", [[52, 0], [60, 10], [89, 100]]), /percent 0/);
  assert.match(validateSide("cpu", [[52, 50], [60, 30], [89, 100]]), /only rise/);
  assert.match(validateSide("cpu", [[52, 0], [52, 30], [89, 100]]), /share a temperature/);
  assert.match(validateSide("cpu", [[52, 0], [60, 30], [80, 60]]), /100 % by 95/);
  assert.match(validateSide("gpu", [[48, 0], [70, 100], [120, 100]]), /20..110/);
  assert.match(validateSide("gpu", [[48, 0], [90, 100]]), /100 % by 85/, "interp at 85 is below 100");
});

test("scale maps both ways", () => {
  const s = makeScale(500, 300);
  assert.ok(Math.abs(s.t(s.x(75)) - 75) < 1e-9);
  assert.ok(Math.abs(s.p(s.y(40)) - 40) < 1e-9);
  assert.ok(s.x(T0) < s.x(T1));
  assert.ok(s.y(100) < s.y(0));
});

test("linePoints extend flat to both ends", () => {
  const l = linePoints([[50, 20], [70, 80]]);
  assert.deepEqual(l[0], [T0, 20]);
  assert.deepEqual(l[l.length - 1], [T1, 80]);
});
