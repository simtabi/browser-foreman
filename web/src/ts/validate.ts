// The page-side twin of src/lib/validate.py: the same rules, so a bad request is caught with a clear message before it
// is sent. The server checks again; this never replaces it.
//   const day = v.text().pattern(/^\d{4}-\d{2}-\d{2}$/, "a date like 2026-11-12");
//   const r = day.check(input.value); if (!r.ok) show(r.error);

export type Result<T> = { ok: true; value: T } | { ok: false; error: string };

export class Rule<T> {
  private constructor(private readonly test: (v: unknown) => Result<T>) {}

  static of<T>(test: (v: unknown) => Result<T>): Rule<T> { return new Rule(test); }

  check(v: unknown): Result<T> { return this.test(v); }

  /** An extra condition; chainable. */
  where(pred: (v: T) => boolean, error: string): Rule<T> {
    return new Rule<T>(v => { const r = this.test(v); return !r.ok ? r : pred(r.value) ? r : { ok: false, error }; });
  }

  pattern(this: Rule<string>, rx: RegExp, error: string): Rule<string> { return this.where(s => rx.test(s), error); }
  max(this: Rule<string>, n: number): Rule<string> { return this.where(s => s.length <= n, `at most ${n} characters`); }
}

const ok = <T>(value: T): Result<T> => ({ ok: true, value });
const fail = <T>(error: string): Result<T> => ({ ok: false, error });

export const v = {
  text: (): Rule<string> => Rule.of(x => typeof x === "string" ? ok(x.trim()) : fail("must be text")),
  webUrl: (): Rule<string> => Rule.of(x => typeof x === "string" && /^https?:\/\/[^\s\x00-\x1f]+$/i.test(x) && x.length <= 4096 ? ok(x) : fail("not a web address")),
  day: (): Rule<string> => v.text().pattern(/^\d{4}-\d{2}-\d{2}$/, "a date like 2026-11-12"),
  snapshotId: (): Rule<string> => v.text().pattern(/^\d{4}-\d{2}-\d{2}_\d{6}$/, "not a snapshot id"),
  tags: (): Rule<string[]> => Rule.of(x => {
    if (typeof x !== "string") return fail("must be text");
    const tags = [...new Set(x.split(/[,\s]+/).map(t => t.trim()).filter(Boolean))];
    return tags.length > 20 ? fail("at most 20 tags") : tags.some(t => t.length > 40) ? fail("a tag is longer than 40 characters") : ok(tags);
  }),
};
