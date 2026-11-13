// Runs in <head>, before the page paints: sets the theme the person chose (or the system's), so a dark page never
// shows light for a moment. The page script (applySettings) takes over from there; this only reads.
(() => {
  let theme = "system";
  try { theme = (JSON.parse(localStorage.getItem("bm-preview-v2:settings") || "{}") as { theme?: string }).theme || "system"; } catch { /* storage off */ }
  const dark = theme === "dark" || (theme === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.setAttribute("data-bs-theme", dark ? "dark" : "light");
})();
