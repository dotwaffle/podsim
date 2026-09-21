"use strict";

const fs = require("node:fs");
const zlib = require("node:zlib");

const source = process.argv[2];
if (!source) throw new Error("Usage: node scripts/compress-wasm.cjs path/to/application.wasm");
const compressed = zlib.gzipSync(fs.readFileSync(source), { level: zlib.constants.Z_BEST_COMPRESSION });
const temporary = `${source}.gz.tmp`;
try {
  fs.writeFileSync(temporary, compressed);
  fs.renameSync(temporary, `${source}.gz`);
} finally {
  fs.rmSync(temporary, { force: true });
}
