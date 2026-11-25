// The parts of the WebExtension APIs this extension uses (Chromium's chrome.*, Firefox's browser.*; both promise-based
// in Manifest V3). Declared here, so the build needs no @types package.
export interface ExtTab { id?: number; url?: string; title?: string; active: boolean; pinned: boolean; groupId?: number; windowId: number; index: number }
export interface ExtWindow { id?: number; incognito: boolean; focused: boolean; tabs?: ExtTab[]; type?: string }
export interface ExtGroup { id: number; windowId: number; title?: string; color: string; collapsed: boolean }
export interface ExtBookmark { id: string; parentId?: string; title: string; url?: string; dateAdded?: number; index?: number; type?: string; children?: ExtBookmark[] }

export interface ExtApi {
  runtime: { getURL(p: string): string; reload(): void; getManifest(): { version: string }; onInstalled: Hook<() => void>; onStartup: Hook<() => void>; onMessage: Hook<(msg: any, sender: unknown, reply: (r: unknown) => void) => boolean | void> ; sendMessage(msg: unknown): Promise<any> };
  windows: { getAll(q: { populate: boolean; windowTypes?: string[] }): Promise<ExtWindow[]>; create(o: { url?: string[]; incognito?: boolean; focused?: boolean }): Promise<ExtWindow>; onRemoved: Hook<() => void>; onCreated: Hook<() => void> };
  tabs: { create(o: { url: string; windowId?: number; active?: boolean }): Promise<ExtTab>; group?(o: { tabIds: number[]; createProperties?: { windowId?: number } }): Promise<number>;
    onUpdated: Hook<(id: number, change: { url?: string; title?: string; status?: string }) => void>; onRemoved: Hook<() => void>; onCreated: Hook<() => void>; onMoved: Hook<() => void>; onAttached: Hook<() => void> };
  tabGroups?: { query(q: object): Promise<ExtGroup[]>; update(id: number, o: { title?: string; color?: string; collapsed?: boolean }): Promise<ExtGroup>; onUpdated: Hook<() => void> };
  bookmarks?: { getTree(): Promise<ExtBookmark[]>; create(o: { parentId: string; index?: number; title: string; url?: string }): Promise<ExtBookmark>;
    update(id: string, o: { title?: string; url?: string }): Promise<ExtBookmark>; move(id: string, o: { parentId: string; index?: number }): Promise<ExtBookmark>;
    remove(id: string): Promise<void>; removeTree(id: string): Promise<void> };
  storage: { local: { get(keys: string[] | string): Promise<Record<string, any>>; set(o: Record<string, unknown>): Promise<void>; remove(keys: string[] | string): Promise<void> } };
  alarms: { create(name: string, o: { periodInMinutes: number }): void; onAlarm: Hook<(a: { name: string }) => void> };
  action?: { setBadgeText(o: { text: string }): Promise<void>; setBadgeBackgroundColor(o: { color: string }): Promise<void>; setTitle(o: { title: string }): Promise<void> };
}
interface Hook<F> { addListener(f: F): void }

/** The browser's API: Firefox and Safari have `browser`, Chromium has `chrome`. */
export const ext = ((globalThis as any).browser ?? (globalThis as any).chrome) as ExtApi;
