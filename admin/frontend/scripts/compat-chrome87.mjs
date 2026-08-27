import { readdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { transform } from 'esbuild';

const chunksRoot = path.resolve('out/_next/static/chunks');

async function listJavaScript(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const absolute = path.join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await listJavaScript(absolute));
    else if (entry.isFile() && entry.name.endsWith('.js')) files.push(absolute);
  }
  return files;
}

const files = await listJavaScript(chunksRoot);
let changed = 0;

for (const file of files) {
  const source = await readFile(file, 'utf8');
  const result = await transform(source, {
    loader: 'js',
    target: 'chrome87',
    minify: true,
    legalComments: 'none',
    charset: 'utf8',
  });
  if (result.code !== source) {
    await writeFile(file, result.code, 'utf8');
    changed += 1;
  }
}

console.log(`Chrome 87 compatibility pass: ${changed}/${files.length} chunks rewritten`);
