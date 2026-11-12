// The browser import file (Netscape bookmark format, which Chrome, Edge, Firefox and Safari all import), built from a
// tree. Pure: the page passes how to read each bookmark and which folder to leave out (the Trash). Moved from the page
// script on 2026-11-12; same output.
//
//   const { text, links } = netscapeFile(root, { heading: "Bookmarks (cleaned 2026-11-12, unified)", skip: bin, ... });

import type { BookmarkId, Folder, TreeNode } from "./tree";
import { isFolder } from "./tree";

export interface ExportReader {
  heading: string;                          // the one folder everything goes into
  skip?: Folder | null;                     // left out with all it holds (the Trash)
  order: (f: Folder) => TreeNode[];         // children in the order shown
  name: (f: Folder) => string;              // a folder's display name
  url: (id: BookmarkId) => string;
  title: (id: BookmarkId) => string;
  added: (id: BookmarkId) => number | undefined;  // Unix seconds
  note: (id: BookmarkId) => string | undefined;   // a bookmark's description
}

/** Escape text for an attribute or element body. */
export const escapeHtml = (s: unknown): string =>
  String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

export function netscapeFile(root: Folder, r: ExportReader): { text: string; links: number } {
  const e = escapeHtml;
  const out = ["<!DOCTYPE NETSCAPE-Bookmark-file-1>", '<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">',
    "<TITLE>Bookmarks</TITLE>", "<H1>Bookmarks</H1>", "<DL><p>", `    <DT><H3>${r.heading}</H3>`, "    <DL><p>"];
  let links = 0;
  const walk = (f: Folder, depth: number): void => {
    const ind = "    ".repeat(depth);
    for (const x of r.order(f)) {
      if (isFolder(x)) {
        if (x === r.skip) continue;
        out.push(`${ind}<DT><H3>${e(r.name(x))}</H3>`);
        if (x.d) out.push(`${ind}<DD>${e(x.d)}`);
        out.push(`${ind}<DL><p>`); walk(x, depth + 1); out.push(`${ind}</DL><p>`);
      } else {
        const note = r.note(x); links++;
        out.push(`${ind}<DT><A HREF="${e(r.url(x))}" ADD_DATE="${r.added(x) || 0}">${e(r.title(x))}</A>`);
        if (note) out.push(`${ind}<DD>${e(note)}`);
      }
    }
  };
  walk(root, 2);
  out.push("    </DL><p>", "</DL><p>", "");
  return { text: out.join("\n"), links };
}
