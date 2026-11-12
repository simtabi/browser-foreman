// A background refresher: runs a task every N milliseconds while the page is visible, never two at once, and
// backs off after failures so a sleeping browser or a stopped server is not hammered.
//
//   const p = new Poller(async () => { … }).every(5000).start();
//   p.every(2000);       // change the pace; takes effect at once
//   p.every(0);          // 0 = off
//   p.now();             // run one now (e.g. when the page becomes visible again)

export class Poller {
  private ms = 0;
  private timer = 0;
  private running = false;
  private failures = 0;
  private started = false;

  constructor(private readonly task: () => Promise<unknown> | unknown, private readonly maxBackoff = 60_000) {
    document.addEventListener("visibilitychange", () => { if (!document.hidden && this.started && this.ms) void this.now(); });
  }

  every(ms: number): this {
    this.ms = Math.max(0, Math.floor(ms) || 0);
    if (this.started) this.schedule();
    return this;
  }

  start(): this { this.started = true; this.schedule(); return this; }
  stop(): this { this.started = false; clearTimeout(this.timer); return this; }
  get pace(): number { return this.ms; }

  async now(): Promise<void> {
    if (this.running) return;
    this.running = true; clearTimeout(this.timer);
    try { await this.task(); this.failures = 0; }
    catch { this.failures++; }
    finally { this.running = false; this.schedule(); }
  }

  private schedule(): void {
    clearTimeout(this.timer);
    if (!this.started || !this.ms) return;
    const wait = Math.min(this.maxBackoff, this.ms * 2 ** Math.min(this.failures, 6));
    this.timer = window.setTimeout(() => { if (document.hidden) this.schedule(); else void this.now(); }, wait);
  }
}
