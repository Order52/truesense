// The window: shell (navigation, live strip, connection state), router, stream.

import { initSession, startStream, onLiveError, call, tryCall } from "./api.js";
import { store, ingest } from "./state.js";
import { h, toast, errText, button } from "./ui.js";
import { icon } from "./icons.js";
import { PAGES, resolveRoute } from "./pages/index.js";
import { fmtTemp, fmtRpm } from "./format.js";

const session = await initSession();
const root = document.getElementById("app");
root.className = "";
root.replaceChildren();

if (!session.hasToken) {
  root.append(h("p", { class: "boot-text", text: session.linkUsed ? "This window link has already been used or has expired. Run `truesense gui` again." : "This page has no access. Start the window with `truesense gui`, which opens it with a one-time link." }));
  throw new Error("no token");
}

onLiveError((e) => toast(errText(e), "error"));

// ---- shell ----
const navLinks = PAGES.map((p) => h("a", { class: "nav-link", href: `#/${p.id}`, "data-page": p.id }, icon(p.icon, 20), h("span", { text: p.nav || p.title })));
const nav = h("nav", { class: "nav", id: "nav", "aria-label": "Pages" }, h("div", { class: "brand" }, h("img", { src: "logo.svg", alt: "" }), h("b", { text: "TrueSense" })), h("div", { class: "nav-links" }, ...navLinks));
const quitBtn = button("Quit", { icon: "power", kind: "quiet", onClick: quit, title: "Close TrueSense's window server" });
const versionLine = h("span", { text: "" });
nav.append(h("div", { class: "nav-foot" }, versionLine, quitBtn));

const menuBtn = button("", { icon: "menu", kind: "quiet", title: "Menu", onClick: () => setNav(!nav.classList.contains("open")) });
menuBtn.classList.add("menu-btn");
menuBtn.setAttribute("aria-controls", "nav");
menuBtn.setAttribute("aria-expanded", "false");
const title = h("h1", { id: "page-title", text: "TrueSense" });
const chipCpu = h("span", { class: "chip" }, "CPU ", h("b", { text: "–" }));
const chipGpu = h("span", { class: "chip" }, "GPU ", h("b", { text: "–" }));
const chipFan = h("span", { class: "chip optional" }, "Fans ", h("b", { text: "–" }));
const chipSrc = h("span", { class: "chip optional" }, h("b", { text: "–" }));
const strip = h("div", { class: "live-strip", "aria-label": "Live readings" }, chipCpu, chipGpu, chipFan, chipSrc);
const topbar = h("header", { class: "topbar" }, menuBtn, title, strip);
const bannerHost = h("div", { id: "banners" });
const main = h("main", { id: "main", tabindex: "-1" });
const skip = h("a", { class: "skip", href: "#main", text: "Skip to content" });
skip.addEventListener("click", (e) => {
  e.preventDefault();
  main.focus();
});
root.append(skip, h("div", { class: "shell" }, nav, h("div", { class: "main-col" }, topbar, bannerHost, main)));

function setNav(open) {
  nav.classList.toggle("open", open);
  menuBtn.setAttribute("aria-expanded", String(open));
}
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && nav.classList.contains("open")) setNav(false);
});
nav.addEventListener("click", (e) => {
  if (e.target.closest("a")) setNav(false);
});

async function quit() {
  try {
    await call("gui", "quit", {});
  } catch {
    /* the server may already be gone */
  }
  root.replaceChildren(h("p", { class: "boot-text", text: "TrueSense has stopped. You can close this window." }));
  stop();
}

// ---- router ----
let current = null;
let routeToken = 0;

async function show() {
  const { page, sub } = resolveRoute(location.hash);
  const mine = ++routeToken;
  if (current && current.destroy) current.destroy();
  current = null;
  navLinks.forEach((a) => (a.dataset.page === page.id ? a.setAttribute("aria-current", "page") : a.removeAttribute("aria-current")));
  title.textContent = page.title;
  document.title = `TrueSense – ${page.title}`;
  main.replaceChildren(h("p", { class: "hint", text: "Loading…" }));
  try {
    const mod = await page.load();
    if (mine !== routeToken) return;
    const view = mod.default({ sub });
    current = view;
    main.replaceChildren(view.el);
    if (view.el.classList.contains("page") === false) view.el.classList.add("page");
  } catch (e) {
    if (mine !== routeToken) return;
    main.replaceChildren(h("div", { class: "notice error", role: "alert" }, icon("warn", 18), h("span", { text: `This page could not be shown: ${errText(e)}` })));
  }
  window.scrollTo(0, 0);
  if (document.activeElement === document.body || document.activeElement === null) main.focus({ preventScroll: true });
}
window.addEventListener("hashchange", show);
document.addEventListener("visibilitychange", () => {
  if (!document.hidden && current && current.refresh) current.refresh();
});

// ---- live state ----
function paintStrip(st) {
  const msg = st.live;
  const s = (msg && msg.sensors) || {};
  chipCpu.lastChild.textContent = fmtTemp(s.cpu_temp);
  chipGpu.lastChild.textContent = fmtTemp(s.gpu_temp);
  chipCpu.classList.toggle("hot", s.cpu_temp >= 85);
  chipGpu.classList.toggle("hot", s.gpu_temp >= 75);
  chipFan.lastChild.textContent = fmtRpm(Math.max(s.cpu_fan_rpm || 0, s.gpu_fan_rpm || 0));
  const src = msg ? (msg.ac === true ? "AC" : msg.ac === false ? "Battery" : "") : "";
  chipSrc.lastChild.textContent = src ? `${src}${msg.battery_pct != null ? ` ${Math.round(msg.battery_pct)} %` : ""}` : "–";
}

let connBanner = null;
function paintConn(st) {
  const msg = {
    offline: ["warn", "Lost the connection to TrueSense. Trying again…"],
    stale: ["error", "This window belongs to an earlier run of TrueSense. Close it and run `truesense gui` again."],
  }[st.conn];
  if (connBanner) connBanner.remove();
  connBanner = null;
  if (msg) {
    connBanner = h("div", { class: `banner ${msg[0]}`, role: "alert" }, icon("warn", 18), h("span", { text: msg[1] }));
    bannerHost.prepend(connBanner);
  }
}

store.subscribe((st) => {
  paintStrip(st);
});
let lastConn = "";
store.subscribe((st) => {
  if (st.conn !== lastConn) {
    lastConn = st.conn;
    paintConn(st);
  }
});

const stop = startStream(ingest, (conn) => store.set({ conn }));

// ---- start-up extras ----
tryCall("version", "get", {}, null).then((v) => {
  if (v && v.truesense) versionLine.textContent = `TrueSense ${v.truesense}`;
});

async function checkUpdate() {
  const auto = await tryCall("update", "auto", { action: "status" }, false);
  if (auto !== true) return;
  const r = await tryCall("update", "check", { force: false }, null);
  if (!r || !r.available) return;
  const banner = h(
    "div",
    { class: "banner", role: "status" },
    icon("info", 18),
    h("span", { text: `TrueSense ${r.latest} is available (you have ${r.current}).` }),
    h("a", { class: "btn primary", href: "#/settings" }, "Update"),
    button("Later", { kind: "quiet", onClick: () => banner.remove() }),
  );
  bannerHost.append(banner);
}
setTimeout(checkUpdate, 6000);

if (!location.hash || location.hash === "#") history.replaceState(null, "", "#/home");
show();
