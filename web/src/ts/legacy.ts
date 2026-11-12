// The page's existing script (preview_template.html) owns the tree, menus, toasts and dialogs. New modules reach it
// only through this typed bridge, so the day a piece moves to TypeScript only this file changes.

export type ToastKind = "info" | "success" | "warning" | "danger";
export interface ConfirmOptions { title: string; body: string; ok?: string; no?: string; danger?: boolean }

export interface Bookmark { id: number; u: string; t: string; a?: number; s: string }

export interface LegacyBridge {
  data: { keep: Bookmark[]; total: number };
  titleOf(id: number): string;
  setAppView(view: "bookmarks" | "tabs" | "timeline"): void;
  selectSnapshot(id: string): Promise<void>;
  refreshTabs(): Promise<void>;
  /** One new window per batch; `browser` pins it to that browser, otherwise every browser chosen to open links. */
  openWindows(batches: { urls: string[]; incognito: boolean; browser?: string }[]): Promise<void>;
}

declare global {
  interface Window {
    BF_LEGACY?: LegacyBridge;
    toast(text: string, kind?: ToastKind, title?: string): void;
    askConfirm(opts: ConfirmOptions): Promise<boolean>;
    tokenPost<T = unknown>(url: string, body: unknown): Promise<T>;
    host(url: string): string;
    shortUrl(url: string, max?: number): string;
  }
}

export const legacy = (): LegacyBridge => {
  const b = window.BF_LEGACY;
  if (!b) throw new Error("the page's script has not started");
  return b;
};
export const toast = (text: string, kind: ToastKind = "info", title = ""): void => window.toast(text, kind, title);
export const confirm = (o: ConfirmOptions): Promise<boolean> => window.askConfirm(o);
export const host = (u: string): string => window.host(u);
export const shortUrl = (u: string, max = 64): string => window.shortUrl(u, max);
