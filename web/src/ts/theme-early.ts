// Runs in <head>, before the page paints: sets the theme the person chose (or the system's), so a dark page never
// shows light for a moment, and whether the loading screen covers the page while it loads. The page script
// (applySettings) takes over from there; this only reads.
(() => {
  let set: { theme?: string; bootLoader?: boolean } = {};
  try { set = JSON.parse(localStorage.getItem("bm-preview-v2:settings") || "{}"); } catch { /* storage off */ }
  const theme = set.theme || "system";
  const dark = theme === "dark" || (theme === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  const root = document.documentElement;
  root.setAttribute("data-bs-theme", dark ? "dark" : "light");
  if (set.bootLoader === false) { root.setAttribute("data-boot", "off"); return; }
  root.classList.add("booting");
  // Never stuck behind it: if the page has not said it is ready in 15 seconds (a script failed, say), lift it anyway.
  setTimeout(() => { root.classList.remove("booting"); document.getElementById("bootLoader")?.classList.add("boot-done"); }, 15000);
})();
