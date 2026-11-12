// A bookmark's detail panel: what it tells you about the link. Which alert shows (dead, moved, updated, not checked,
// maybe a "not found" page), which badges, which message stands in for a missing snapshot, and whether the page may be
// loaded live in the frame. Pure rules: the page draws them (alertBox, badge) and wires each action. Moved from the
// page script on 2026-11-12; same wording, same order.

import { type Bookmark, pendingUpdate } from "./tree-rows";

/** The bookmark fields the panel reads, beyond a row's. */
export interface LinkItem extends Bookmark {
  n?: string;            // the check's own note ("HTTP 404", …)
  nk?: string;           // "other": the new address is on another site
  prev?: string;         // the address before an applied update
  sx?: string;           // why it may be a soft 404
  h?: string;            // snapshot hash
}

/** What an alert's button does; the page maps each to its action. */
export type LinkAction = "recheck" | "putBack" | "trash" | "update" | "openNew" | "revert" | "retry" | "open";
export interface AlertSpec { tone: string; icon: string; title: string; body: string; actions: [string, string, LinkAction, string?][] }

export interface AlertContext {
  readOnly: boolean; inTrash: boolean;
  help: Record<string, string>; statusName: Record<string, string>; statusIcon: Record<string, string>;
}

const QUIET = ["unverified", "needs-login", "local", "skipped", "n/a"];

/** The alert at the top of a link's panel, or null when there is nothing to say. Dead first, then a pending move, an
 *  applied update, a status that was not checked, a possible "not found" page. */
export function linkAlert(i: LinkItem, c: AlertContext): AlertSpec | null {
  const note = i.n && i.s !== "ok" ? ` Check result: ${i.n}.` : "";
  if (i.s === "dead") return {
    tone: "danger", icon: "ti-alert-triangle",
    title: i.dk === "host" ? "Dead: the whole site is gone" : "Dead: the page is gone, the site is still up",
    body: (c.help.dead ?? "").split(". ")[0] + "." + note,
    actions: c.readOnly ? [] : [["Re-check", "ti-refresh", "recheck"], c.inTrash ? ["Put back", "ti-arrow-back-up", "putBack"] : ["Move to Trash", "ti-trash", "trash", "btn-ghost-danger"]],
  };
  if (pendingUpdate(i)) {
    const other = i.nk === "other";
    return {
      tone: other ? "purple" : "warning", icon: "ti-arrow-forward-up",
      title: other ? "Now goes to another site" : "Moved to a new address",
      body: (other ? "Check it first: it may be a rebrand, or a parked or taken-over domain. " : "") + "New address: " + i.nu,
      actions: c.readOnly ? [] : [["Update to the new address", "ti-check", "update", other ? "" : "btn-warning"], ["Open the new address", "ti-external-link", "openNew"]],
    };
  }
  if (i.applied) return { tone: "success", icon: "ti-circle-check", title: "Updated to the new address", body: "Was: " + i.prev, actions: [["Revert", "ti-arrow-back", "revert"]] };
  if (QUIET.includes(i.s)) return {
    tone: i.s === "needs-login" ? "info" : "secondary", icon: c.statusIcon[i.s] || "ti-info-circle",
    title: c.statusName[i.s] ?? i.s, body: (c.help[i.s] || "") + note,
    actions: i.s === "unverified" && !c.readOnly ? [["Try again", "ti-refresh", "retry"]] : [],
  };
  if (i.sx) return { tone: "warning", icon: "ti-help-circle", title: "Might be a “not found” page", body: i.sx + ". Open it to check.", actions: [["Open", "ti-external-link", "open"]] };
  return null;
}

/** The badges under the title: [text, class, icon, help key]. */
export function linkBadges(i: LinkItem, hasNote: boolean): [string, string, string, string?][] {
  const out: [string, string, string, string?][] = [];
  if (i.s === "ok") out.push(["Working", "bg-green-lt", "ti-circle-check", "ok"]);
  if (i.p) out.push(["Untouched folder", "bg-green-lt", "ti-lock", "prot"]);
  if (i.m) out.push(["Filed here because it matches this folder", "bg-green-lt", "ti-arrow-merge"]);
  if (hasNote) out.push(["Your note", "bg-blue-lt", "ti-notes"]);
  return out;
}

/** What shows where the page's snapshot would be, when there is none. */
export function snapshotMessage(i: LinkItem): string {
  if (i.s === "dead") return "Dead link, so there is no snapshot. Refresh (R) re-checks it.";
  if (!/^https?:/i.test(i.u)) return "Not a web page, so there is no snapshot.";
  if (["local", "skipped"].includes(i.s)) return "Not visited on purpose (local, sign-out or action link). Live (L) loads it here.";
  return NO_SNAPSHOT;
}
export const NO_SNAPSHOT = "No snapshot yet. Refresh (R) takes one; Live (L) loads the page here.";

/** May this address load live in the frame? Never this tool's own address or a local one: the frame's sandbox would
 *  then grant it this page's powers. "web" = not a web page (nothing happens); "local" = refused with a message. */
export function liveLoad(u: string, isSelf: (u: string) => boolean): "ok" | "web" | "local" {
  if (!/^https?:/i.test(u)) return "web";
  if (isSelf(u) || /^https?:\/\/(localhost|127\.|\[::1\])/i.test(u)) return "local";
  return "ok";
}
