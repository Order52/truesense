import test from "node:test";
import assert from "node:assert/strict";
import { createLatest, debounce } from "../web/js/latest.js";

const gate = () => {
  let release;
  const p = new Promise((r) => (release = r));
  return { p, release };
};

test("only the newest request waits behind a running one", async () => {
  const l = createLatest();
  const ran = [];
  const g = gate();
  l.submit(async () => { ran.push(1); await g.p; });
  l.submit(async () => { ran.push(2); });
  l.submit(async () => { ran.push(3); });
  l.submit(async () => { ran.push(4); });
  assert.ok(l.busy());
  g.release();
  await l.idle();
  assert.deepEqual(ran, [1, 4], "2 and 3 were superseded");
  assert.ok(!l.busy());
});

test("an idle runner starts at once and a failure is reported but does not stop the next", async () => {
  const errs = [];
  const l = createLatest((e) => errs.push(e.message));
  const ran = [];
  const g = gate();
  l.submit(async () => { await g.p; throw new Error("boom"); });
  l.submit(async () => { ran.push("next"); });
  g.release();
  await l.idle();
  assert.deepEqual(errs, ["boom"]);
  assert.deepEqual(ran, ["next"]);
  l.submit(async () => { ran.push("again"); });
  await l.idle();
  assert.deepEqual(ran, ["next", "again"]);
});

test("idle resolves at once when nothing runs", async () => {
  await createLatest().idle();
});

test("debounce collapses a burst and can be cancelled", () => {
  const timers = [];
  const fake = { set: (fn) => (timers.push(fn), timers.length), clear: (id) => (timers[id - 1] = null) };
  const calls = [];
  const d = debounce((v) => calls.push(v), 100, fake);
  d(1); d(2); d(3);
  timers.forEach((t) => t && t());
  assert.deepEqual(calls, [3]);
  timers.length = 0;
  d(4);
  d.cancel();
  timers.forEach((t) => t && t());
  assert.deepEqual(calls, [3]);
});
