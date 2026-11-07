import test from "node:test";
import assert from "node:assert/strict";
import { createSSEParser } from "../web/js/sse.js";

const collect = () => {
  const got = [];
  return { got, p: createSSEParser((e) => got.push(e)) };
};

test("parses events, comments and the retry line", () => {
  const { got, p } = collect();
  p.push("retry: 2000\n\n: ping\n\nevent: state\ndata: {\"a\":1}\n\n");
  assert.deepEqual(got, [{ event: "state", data: '{"a":1}' }]);
});

test("an event split over chunks (even in the middle of a line) is assembled", () => {
  const { got, p } = collect();
  for (const part of ["event: sta", "te\nda", 'ta: {"cpu"', ":50}\n", "\nevent: state\ndata: 2\n\n"]) p.push(part);
  assert.deepEqual(got, [{ event: "state", data: '{"cpu":50}' }, { event: "state", data: "2" }]);
});

test("multi-line data joins with a newline; CRLF and lone CR are line ends", () => {
  const { got, p } = collect();
  p.push("data: a\r\ndata: b\r\n\r\ndata: c\r\rdata: d\n\n");
  assert.deepEqual(got, [{ event: "message", data: "a\nb" }, { event: "message", data: "c" }, { event: "message", data: "d" }]);
});

test("a CR LF pair split across chunks is one line end", () => {
  const { got, p } = collect();
  p.push("data: x\r");
  p.push("\n\r\n");
  assert.deepEqual(got, [{ event: "message", data: "x" }]);
});

test("an event without data is not delivered", () => {
  const { got, p } = collect();
  p.push("event: nothing\n\n");
  assert.deepEqual(got, []);
});

test("however the bytes are chunked, the same events come out", () => {
  const stream = ": ping\r\n\r\nretry: 2000\n\nevent: state\ndata: {\"a\":1}\n\nevent: state\r\ndata: {\"b\":\r\ndata: 2}\r\n\r\ndata: lone\r\rdata: last\n\n";
  const whole = collect();
  whole.p.push(stream);
  assert.ok(whole.got.length >= 3);
  let seed = 12345;
  const rnd = () => (seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff;
  for (let round = 0; round < 200; round++) {
    const c = collect();
    for (let i = 0; i < stream.length; ) {
      const n = 1 + Math.floor(rnd() * 7);
      c.p.push(stream.slice(i, i + n));
      i += n;
    }
    assert.deepEqual(c.got, whole.got, `round ${round}`);
  }
});
