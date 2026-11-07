import test from "node:test";
import assert from "node:assert/strict";
import { createHistory, sparkPath } from "../web/js/spark.js";
import { createStore } from "../web/js/store.js";

test("history keeps the newest samples and turns junk into gaps", () => {
  const h = createHistory(3);
  [1, 2, 3, 4].forEach((v) => h.push(v));
  assert.deepEqual(h.values(), [2, 3, 4]);
  h.push(undefined);
  assert.deepEqual(h.values(), [3, 4, null]);
  assert.equal(h.last(), null);
});

test("sparkline path spans the box and skips gaps", () => {
  assert.equal(sparkPath([1], 100, 20), "");
  assert.equal(sparkPath([null, null], 100, 20), "");
  const d = sparkPath([0, 50, 100], 100, 24, { min: 0, max: 100 });
  assert.match(d, /^M0\.0 22\.0L50\.0 12\.0L100\.0 2\.0$/);
  const gap = sparkPath([10, null, 20], 100, 24);
  assert.equal((gap.match(/M/g) || []).length, 1);
  assert.equal((gap.match(/L/g) || []).length, 1);
});

test("a flat line is drawn level, not stretched", () => {
  const d = sparkPath([50, 50, 50], 100, 24);
  const ys = [...d.matchAll(/ (\d+\.\d)/g)].map((m) => m[1]);
  assert.equal(new Set(ys).size, 1);
});

test("store notifies subscribers with the merged state until they leave", () => {
  const s = createStore({ a: 1 });
  const seen = [];
  const off = s.subscribe((st) => seen.push(st));
  s.set({ b: 2 });
  off();
  s.set({ c: 3 });
  assert.deepEqual(seen, [{ a: 1, b: 2 }]);
  assert.deepEqual(s.get(), { a: 1, b: 2, c: 3 });
});
