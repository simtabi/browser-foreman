// Lifts the loading screen (template.html #bootLoader) once everything is ready: the page script has drawn its first
// view (main.ts calls this after it and Alpine have started), the fonts and stylesheets have loaded, and the browser
// has painted twice. theme-early.ts holds a 15-second fallback, and data-boot="off" (the setting) hides it from the
// start.

/** Resolves when the window's load event has fired (now, when it already has). */
const loaded = (): Promise<void> =>
  document.readyState === "complete" ? Promise.resolve() : new Promise(r => window.addEventListener("load", () => r(), { once: true }));

/** Two animation frames: what was drawn has been painted. A page the browser is not drawing (a background tab, a
 *  hidden window) runs no frames, so a short timer stands in, and the page is ready when it is first shown. */
const painted = (): Promise<void> => new Promise(r => {
  requestAnimationFrame(() => requestAnimationFrame(() => r()));
  setTimeout(r, 150);
});

/** Lifts the loading screen once the page is ready. Waits at most limit ms for fonts and the load event. */
export async function bootReady(limit = 8000): Promise<void> {
  const box = document.getElementById("bootLoader");
  if (!box) return;
  const msg = document.getElementById("bootMsg");
  if (msg) msg.textContent = "Almost ready…";
  await Promise.race([
    Promise.all([loaded(), document.fonts?.ready ?? Promise.resolve()]),
    new Promise(r => setTimeout(r, limit)),
  ]);
  await painted();
  performance.mark?.("bf:ready");  // when the page was ready, for the browser's performance tools
  document.documentElement.classList.remove("booting");
  box.setAttribute("aria-busy", "false");
  box.classList.add("boot-done");
  setTimeout(() => box.remove(), 400);  // after the fade: nothing left on top of the page
}
