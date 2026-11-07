import test from "node:test";
import assert from "node:assert/strict";
import { normHex, hexToRgb, rgbToHex, lerpRgb, hsv, luminance, textOn, gradientAt, rainbowAt, STOCK_CELLS, cellsFromPixels, pixelsFromCells, PALETTE } from "../web/js/color.js";

test("normHex accepts what people type", () => {
  assert.equal(normHex("#FF3B30"), "ff3b30");
  assert.equal(normHex(" ff3b30 "), "ff3b30");
  assert.equal(normHex("f80"), "ff8800");
  for (const bad of ["", "zzzzzz", "12345", "1234567", null, undefined, "#ff3b3g"]) assert.equal(normHex(bad), null, String(bad));
});

test("rgb conversions round-trip and clamp", () => {
  assert.deepEqual(hexToRgb("00aec7"), [0, 174, 199]);
  assert.equal(rgbToHex([0, 174, 199]), "00aec7");
  assert.equal(rgbToHex([300, -5, 12.4]), "ff000c");
  assert.deepEqual(lerpRgb([0, 0, 0], [255, 100, 50], 0.5), [128, 50, 25]);
});

test("hsv ramp matches keymap.hsv at the corners", () => {
  assert.deepEqual(hsv(0), [255, 0, 0]);
  assert.deepEqual(hsv(1 / 3), [0, 255, 0]);
  assert.deepEqual(hsv(2 / 3), [0, 0, 255]);
  assert.deepEqual(hsv(1), hsv(0));
  assert.deepEqual(hsv(-0.5), hsv(0.5));
});

test("label colour contrasts with the key", () => {
  assert.equal(textOn("ffffff"), "#10131a");
  assert.equal(textOn("000000"), "#f2f5fa");
  assert.ok(luminance("ffffff") > luminance("808080"));
});

test("gradient and rainbow follow the x positions", () => {
  assert.deepEqual(gradientAt([0, 5, 10], "000000", "ffffff"), ["000000", "808080", "ffffff"]);
  assert.deepEqual(gradientAt([3, 3], "ff0000", "00ff00"), ["ff0000", "ff0000"]);
  const r = rainbowAt([0, 10]);
  assert.equal(r[0], "ff0000");
  assert.notEqual(r[1], "ff0000");
  assert.deepEqual(rainbowAt([4]), ["ff0000"]);
});

test("stock cells are 126 cyan keys and two dark cells", () => {
  assert.equal(STOCK_CELLS.length, 128);
  assert.equal(STOCK_CELLS[0], "00aec7");
  assert.equal(STOCK_CELLS[125], "00aec7");
  assert.equal(STOCK_CELLS[126], "000000");
  assert.equal(PALETTE.length, 16);
});

test("a picture becomes cells column by column and back", () => {
  const cols = 22, rows = 6;
  const px = new Uint8ClampedArray(cols * rows * 4).fill(255);
  // pixel at column 2, row 3 is red; one transparent pixel stays dark
  const red = (3 * cols + 2) * 4;
  px.set([255, 0, 0, 255], red);
  px.set([200, 200, 200, 0], 0);
  const cells = cellsFromPixels(px);
  assert.equal(cells.length, 128);
  assert.equal(cells[2 * 6 + 3], "ff0000");
  assert.equal(cells[0], "000000");
  assert.equal(cells[126], "000000");
  const back = pixelsFromCells(cells);
  assert.deepEqual([...back.slice(red, red + 4)], [255, 0, 0, 255]);
});
