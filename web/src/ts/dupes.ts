// The rules of the Find duplicates dialog (v1-ui.ts), without the DOM: which groups start ticked, and the trash
// changes that keep one bookmark of each ticked group. Groups come from GET …/duplicates (Go: dupes.Find).

export interface DMember { id: string; title: string; url: string; path: string; added?: number }
export interface DGroup { reasons: string[]; keep: string; members: DMember[] }
export interface TrashOp { op: "trash"; ids: string[]; reason: string }

/** A group found only by title starts unticked: two different pages can share a title. */
export const startsTicked = (g: DGroup): boolean => g.reasons[0] !== "similar-title";

/** One trash change per ticked group: every bookmark but the one kept (the owner's choice, else the server's), with
 *  the reason "duplicate of <kept>" (merge.DuplicateOf), so the copies join Duplicates. Groups with nothing to move are
 *  left out. */
export function trashOps(groups: DGroup[], keep: Map<DGroup, string>): TrashOp[] {
  const out: TrashOp[] = [];
  for (const g of groups) {
    const k = keep.get(g) ?? g.keep;
    const ids = g.members.map(m => m.id).filter(id => id !== k);
    if (ids.length) out.push({ op: "trash", ids, reason: `duplicate of ${k}` });
  }
  return out;
}
