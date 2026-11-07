import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { buildLayout, keyAt, cellsInBox, groupCells, xOfCell, neighbour, keyLabel } from "../web/js/kbd-layout.js";

const nameCells = JSON.parse(readFileSync(new URL("../guitest/layout.json", import.meta.url))).keys;
const L = buildLayout(nameCells);
const byName = (n) => L.keys.find((k) => k.name === n);

test("every lit cell of the real keyboard gets exactly one key", () => {
  const cells = L.keys.flatMap((k) => k.cells);
  assert.equal(new Set(cells).size, cells.length, "no cell twice");
  for (const n of ["esc", "w", "space", "predator", "numenter", "up"]) assert.ok(byName(n), n);
  assert.equal(L.width, 20.5);
  assert.equal(L.height, 6.4, "nothing needed an extra row");
});

test("keys do not overlap", () => {
  for (const a of L.keys)
    for (const b of L.keys) {
      if (a === b) continue;
      const overlap = a.x < b.x + b.w - 0.01 && b.x < a.x + a.w - 0.01 && a.y < b.y + b.h - 0.01 && b.y < a.y + a.h - 0.01;
      assert.ok(!overlap, `${a.name} overlaps ${b.name}`);
    }
});

test("an unknown key still gets a place below the board", () => {
  const l = buildLayout({ ...nameCells, mystery: [127] });
  const k = l.keys.find((x) => x.name === "mystery");
  assert.ok(k && k.y >= 6.4);
  assert.ok(l.height > 6.4);
});

test("hit tests and box selection use layout units", () => {
  const w = byName("w");
  assert.equal(keyAt(L.keys, w.x + 0.5, w.y + 0.5).name, "w");
  assert.equal(keyAt(L.keys, -1, -1), null);
  const cells = cellsInBox(L.keys, { x: w.x + 0.5, y: w.y + 0.5 }, { x: w.x + 0.6, y: w.y + 0.6 });
  assert.deepEqual(cells, w.cells);
  const wide = cellsInBox(L.keys, { x: 30, y: 30 }, { x: -1, y: -1 });
  assert.equal(new Set(wide).size, L.keys.flatMap((k) => k.cells).length);
});

test("groups come from the key names", () => {
  assert.equal(groupCells("all", nameCells).length, 126);
  assert.equal(groupCells("wasd", nameCells).length, 4);
  assert.equal(groupCells("arrows", nameCells).length, 4);
  assert.equal(groupCells("fkeys", nameCells).length, 12);
  assert.equal(groupCells("numrow", nameCells).length, 10);
  assert.equal(groupCells("letters", nameCells).length, 26);
  assert.equal(groupCells("numpad", nameCells).length, 14, "the keys named num*: digits, num/, num., numenter, numlock");
  assert.deepEqual(groupCells("nonsense", nameCells), []);
});

test("x of a cell is its key's centre; arrow navigation walks neighbours", () => {
  const q = byName("q");
  assert.equal(xOfCell(L.keys, q.cells[0]), q.x + q.w / 2);
  assert.equal(xOfCell(L.keys, 9999), 0);
  assert.equal(neighbour(L.keys, byName("w"), "left").name, "q");
  assert.equal(neighbour(L.keys, byName("w"), "right").name, "e");
  assert.equal(neighbour(L.keys, byName("s"), "up").name === "w" || neighbour(L.keys, byName("s"), "up").name === "e" || neighbour(L.keys, byName("s"), "up").name === "q", true);
  assert.equal(neighbour(L.keys, byName("esc"), "left"), null);
  assert.equal(neighbour(L.keys, byName("esc"), "down").name !== "esc", true);
});

test("labels", () => {
  assert.equal(keyLabel("w"), "W");
  assert.equal(keyLabel("backspace"), "⌫");
  assert.equal(keyLabel("f5"), "F5");
  assert.equal(keyLabel("space"), "");
  assert.equal(keyLabel("options"), "Opt");
});
