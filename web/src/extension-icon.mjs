// Draws the extension's icon, the web interface's logo mark (a rounded blue square holding a white bookmark), as a
// PNG of any size, with no dependency: shapes are tested per pixel on a 4×4 grid and written with Node's zlib.
import { deflateSync } from "node:zlib";

const BLUE = [6, 111, 209], WHITE = [255, 255, 255];

// In the mark's own 64-unit square: inside the rounded square (corner radius 14)?
function inSquare(x, y) {
  if (x < 0 || y < 0 || x > 64 || y > 64) return false;
  const r = 14, cx = Math.min(Math.max(x, r), 64 - r), cy = Math.min(Math.max(y, r), 64 - r);
  return (x - cx) ** 2 + (y - cy) ** 2 <= r * r;
}
// Inside the bookmark: x 18–46 from y 14 down to y 50, with a notch rising to y 41 at the middle, corners rounded 4.
function inBookmark(x, y) {
  if (x < 18 || x > 46 || y < 14) return false;
  if (y < 18) { // rounded top corners
    if (x < 22 && (x - 22) ** 2 + (y - 18) ** 2 > 16) return false;
    if (x > 42 && (x - 42) ** 2 + (y - 18) ** 2 > 16) return false;
  }
  const notch = 50 - 9 * (1 - Math.abs(x - 32) / 14); // 41 at the middle, 50 at the sides
  return y <= notch;
}

const crcTable = Array.from({ length: 256 }, (_, n) => { let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; return c >>> 0; });
const crc32 = buf => { let c = 0xffffffff; for (const b of buf) c = crcTable[(c ^ b) & 0xff] ^ (c >>> 8); return (c ^ 0xffffffff) >>> 0; };
function chunk(type, data) {
  const len = Buffer.alloc(4); len.writeUInt32BE(data.length);
  const td = Buffer.concat([Buffer.from(type), data]);
  const crc = Buffer.alloc(4); crc.writeUInt32BE(crc32(td));
  return Buffer.concat([len, td, crc]);
}

/** The icon as a PNG, size × size pixels, RGBA. */
export function iconPNG(size) {
  const raw = Buffer.alloc(size * (size * 4 + 1));
  const S = 4;
  for (let py = 0; py < size; py++) {
    raw[py * (size * 4 + 1)] = 0; // no filter
    for (let px = 0; px < size; px++) {
      let sq = 0, bm = 0;
      for (let sy = 0; sy < S; sy++) for (let sx = 0; sx < S; sx++) {
        const x = ((px + (sx + 0.5) / S) / size) * 64, y = ((py + (sy + 0.5) / S) / size) * 64;
        if (inSquare(x, y)) { sq++; if (inBookmark(x, y)) bm++; }
      }
      const a = sq / (S * S), w = sq ? bm / sq : 0;
      const o = py * (size * 4 + 1) + 1 + px * 4;
      for (let c = 0; c < 3; c++) raw[o + c] = Math.round(BLUE[c] * (1 - w) + WHITE[c] * w);
      raw[o + 3] = Math.round(a * 255);
    }
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0); ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; ihdr[9] = 6; ihdr[10] = 0; ihdr[11] = 0; ihdr[12] = 0; // 8-bit RGBA
  return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), chunk("IHDR", ihdr), chunk("IDAT", deflateSync(raw)), chunk("IEND", Buffer.alloc(0))]);
}
