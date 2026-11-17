// Exercise the actual Go application, not a second Node implementation.
import {execFileSync} from 'node:child_process'
import {mkdtempSync, writeFileSync, readFileSync, rmSync} from 'node:fs'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
import {fileURLToPath} from 'node:url'
const root = fileURLToPath(new URL('../..', import.meta.url))
const scratch = mkdtempSync(join(tmpdir(), 'arcmd-mdx-test-'))
const bin = join(scratch, process.platform === 'win32' ? 'arcmd.exe' : 'arcmd')
execFileSync('go', ['build', '-o', bin, './cmd/arcmd'], {cwd: root})
process.on('exit', () => rmSync(scratch, {recursive: true, force: true}))
export function runCheck(args) {
  return execFileSync(bin, ['--jsx-check', ...args], {env: {...process.env, PATH: '/no-node'}, maxBuffer: 32 * 1024 * 1024, stdio: ['ignore', 'pipe', 'pipe']})
}
export async function compileDocument(source) {
  const path = join(scratch, 'fresh.mdx')
  writeFileSync(path, source)
  try { return JSON.parse(runCheck([path])) }
  catch (error) {
    // The CLI intentionally returns nonzero for partial/source-only manifests.
    // Preserve that manifest for assertions; syntax failures have no JSON output.
    if (error.stdout?.length) return JSON.parse(error.stdout)
    throw new Error(String(error.stderr || error.message))
  }
}
export async function islandHTML(source) {
  const m = await compileDocument(source)
  if (m.blocks.length !== 1 || m.blocks[0].error) throw new Error(m.blocks[0]?.error || 'Expected one island')
  return m.blocks[0].html
}
export const islandStyle = readFileSync(join(root, 'internal/jsx/assets/style.css'), 'utf8')
// Evaluate the exact production Go script body, rather than a JS reimplementation.
const theme = readFileSync(join(root, 'internal/jsx/theme.go'), 'utf8').match(/return `([^`]+)` \+ string\(payload\) \+ `([^`]+)`/)
if (!theme) throw new Error('Cannot locate production theme script')
export function applyTheme(document, payload) {
  document.defaultView.eval(theme[1] + JSON.stringify(payload) + theme[2])
}
