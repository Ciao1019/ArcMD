import {compileSync,createProcessor} from '@mdx-js/mdx'
function fence(source, language) {
 const ticks='`'.repeat(Math.max(3,...[...source.matchAll(/`+/g)].map(m=>m[0].length+1)))
 return `${ticks}${language}\n${source}\n${ticks}`
}
function nested(node) {
 return node.children?.some(c=>c.type==='mdxTextExpression'||c.type==='mdxFlowExpression'||((c.type==='mdxJsxTextElement'||c.type==='mdxJsxFlowElement')&&(c.name===null||/^[A-Z]/.test(c.name)))||nested(c))
}
function imports(node) {
 if(!node||typeof node!=='object')return false
 if(node.type==='ImportExpression'||node.type==='ImportDeclaration')return true
 return Object.values(node).some(v=>Array.isArray(v)?v.some(imports):imports(v))
}
export function compileMDX(input) {
 const {source,namespace}=JSON.parse(input)
 const tree=createProcessor().parse(source)
 let markdown='',cursor=0
 const blocks=[],diagnostics=[]
 for(const n of tree.children){
  const island=n.type==='mdxJsxFlowElement'&&(n.name===null||/^[A-Z]/.test(n.name))
  const unsupported=n.type==='mdxjsEsm'||n.type==='mdxFlowExpression'
  if(!island&&!unsupported){if(nested(n))throw new Error('Inline/nested MDX outside an island is unsupported');continue}
  const start=n.position.start.offset,end=n.position.end.offset,raw=source.slice(start,end)
  markdown+=source.slice(cursor,start)
  if(unsupported){diagnostics.push('Imports/exports/shared expressions are source-only');markdown+=fence(raw,'mdx-source-only-'+namespace)}
  else {
   if(blocks.length>=32)throw new Error('Maximum 32 islands')
   const language=`arcmd-jsx-${namespace}-${blocks.length}`
   const block={language,source:raw,height:240,html:'',error:'',compiled:''}
   try {
    if(imports(n.data?.estree)||imports(n))throw new Error('Document module imports are unsupported')
    block.compiled=String(compileSync(raw,{development:false,outputFormat:'function-body'}))
   } catch(e){block.error=String(e.message);diagnostics.push(block.error)}
   blocks.push(block);markdown+=fence(raw,language)
  }
  cursor=end
 }
 markdown+=source.slice(cursor)
 return {version:1,namespace,markdown,blocks,diagnostics}
}
