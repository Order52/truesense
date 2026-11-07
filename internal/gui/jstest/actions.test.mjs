import test from "node:test";
import assert from "node:assert/strict";
import { ACTION_TYPES, parseSpec, buildSpec, defaultsFor, typeOf } from "../web/js/actions.js";

test("every fixed action round-trips", () => {
  for (const t of ACTION_TYPES.filter((x) => x.kind === "fixed")) {
    const st = parseSpec(t.base);
    assert.equal(st.id, t.id, t.base);
    assert.equal(buildSpec(st), t.base);
  }
});

test("actions with a detail round-trip", () => {
  for (const spec of [
    "power:turbo", "power-cycle:eco,balanced,turbo", "auto:smart", "fan:smart", "fan:max", "fan:auto", "fan:65", "profile:gaming", "look:native",
    "exec:notify-send hello", "exec:sh '/home/me/a script.sh'", "notify:hello there",
  ]) assert.equal(buildSpec(parseSpec(spec)), spec, spec);
});

test("the detail is read into the right field", () => {
  assert.deepEqual(parseSpec("fan:65"), { id: "fan", choice: "manual %", percent: 65, text: "" });
  assert.equal(parseSpec("fan:max").choice, "max");
  assert.equal(parseSpec("power:eco").choice, "eco");
  assert.equal(parseSpec("exec:ls -l").text, "ls -l");
  assert.equal(parseSpec("notify:hi").text, "hi");
});

test("something unknown is kept as a command, not lost", () => {
  const st = parseSpec("mystery-action");
  assert.equal(st.id, "exec");
  assert.equal(st.text, "mystery-action");
  assert.equal(parseSpec(undefined).id, "none");
});

test("cycle lists lose spaces; a new type starts with a sensible detail", () => {
  assert.equal(buildSpec({ id: "power-cycle", text: "eco, quiet , turbo" }), "power-cycle:eco,quiet,turbo");
  assert.equal(buildSpec(defaultsFor("power")), "power:balanced");
  assert.equal(buildSpec(defaultsFor("fan")), "fan:smart");
  assert.equal(buildSpec(defaultsFor("power-cycle")), "power-cycle:eco,balanced,turbo");
  assert.equal(buildSpec(defaultsFor("profile", { profiles: ["gaming"] })), "profile:gaming");
  assert.equal(buildSpec(defaultsFor("look", {})), "look:");
  assert.equal(typeOf("nonsense").id, "none");
});

test("the manual fan percent is rounded", () => {
  assert.equal(buildSpec({ id: "fan", choice: "manual %", percent: 64.6 }), "fan:65");
});
