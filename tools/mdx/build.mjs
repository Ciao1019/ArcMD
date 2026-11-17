import {build} from 'esbuild'
import {fileURLToPath} from 'node:url'
import {resolve} from 'node:path'
const root = fileURLToPath(new URL('../..', import.meta.url))
const dep = resolve(root, 'tools/mdx/node_modules')
const common = {absWorkingDir: root, bundle: true, platform: 'browser', target: 'es2020', legalComments: 'inline'}
await build({...common,
  entryPoints: ['tools/mdx/compiler-entry.mjs'], outfile: 'internal/jsx/assets/mdx-compiler.js', format: 'esm',
  nodePaths: [dep], alias: {'decode-named-character-reference': resolve(dep, 'decode-named-character-reference/index.js')}
})
await build({...common,
  stdin: {contents: "import React from 'react';import {createRoot} from 'react-dom/client';import {jsx,jsxs,Fragment} from 'react/jsx-runtime';export {React,createRoot,jsx,jsxs,Fragment}", resolveDir: resolve(root, 'tools/mdx'), loader: 'js'},
  outfile: 'internal/jsx/assets/react-runtime.js', format: 'iife', globalName: 'ArcMDReact', minify: true,
  define: {'process.env.NODE_ENV': '"production"'}
})
