// Development-only source extraction. No JavaScript ships in the native UI.
// Call generateGhostAssets(page, absoluteRepo, absoluteEsbuild) from ego-browser.
import { mkdtemp, mkdir, writeFile, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

const commit = '387b049ca8fe97b5d72999b5bf61a19ab1ccff81';
const sourceURL = `https://raw.githubusercontent.com/Jakubantalik/Libraries.dev/${commit}/packages/bot-avatars/`;
const entry = `
import { Sim, restPose } from './src/engine';
import { draw, LIGHT_DEFAULTS } from './src/draw';
import { botAvatarPresets } from './src/presets';
import { SHAPE_PATHS } from './src/shapes';
import { autoInk, shade, richer } from './src/color';
export { Sim, restPose, draw };
export function ghostConfig() {
 const preset = botAvatarPresets.ghost;
 const color = richer(shade(preset.color, 0, .25), 0);
 return { ...preset, color, ink:autoInk(color), path:new Path2D(SHAPE_PATHS.ghost),
 shading:'fabric', ...LIGHT_DEFAULTS.fabric, depth:.65, roundness:1,
 fur:{length:1,density:1.6,fuzz:.9,clumps:.4,curl:.7,gravity:.9},
 vivid:0, typeKey:'ghost', still:true, dpr:2,
 whirl:{strength:0,size:1,width:1,length:1,tilt:1} };
}
`;

export async function generateGhostAssets(page, repo, esbuild) {
 const dir = await mkdtemp(join(tmpdir(), 'arcmd-ghost-'));
 await mkdir(join(dir, 'src'));
 const files = ['engine.ts','draw.ts','presets.ts','shapes.ts','color.ts','plastic.ts','mesh.ts','wear.ts'];
 await Promise.all(files.map(async file => {
  const response = await fetch(sourceURL + 'src/' + file);
  if (!response.ok) throw new Error(`Source ${file}: HTTP ${response.status}`);
  await writeFile(join(dir,'src',file), await response.text());
 }));
 await writeFile(join(dir,'reference.ts'), entry);
 await promisify(execFile)(esbuild,[join(dir,'reference.ts'),'--bundle','--format=iife','--global-name=BotReference','--outfile='+join(dir,'reference.js')]);
 await page.evaluate(await readFile(join(dir,'reference.js'),'utf8')+';window.BotReference=BotReference;');
 const assets=join(repo,'internal/app/assets/ghost');
 const fixtures=join(repo,'internal/app/testdata/qabot');
 await mkdir(assets,{recursive:true});await mkdir(fixtures,{recursive:true});
 const png = async (file, data) => writeFile(file,Buffer.from(data.split(',')[1],'base64'));
 for (let pitch=-45;pitch<=30;pitch+=5) {
  const data=await page.evaluate(pitch=>{
   const cfg=BotReference.ghostConfig();cfg.ink='transparent';
   const canvas=document.createElement('canvas');canvas.width=canvas.height=96;const ctx=canvas.getContext('2d');
   const sheet=document.createElement('canvas');sheet.width=1440;sheet.height=320;const out=sheet.getContext('2d');
   for(let yaw=0;yaw<72;yaw++) {
    const pose={...BotReference.restPose('default'),yaw:yaw*5*Math.PI/180,pitch:pitch*Math.PI/180};
    ctx.setTransform(2,0,0,2,0,0);BotReference.draw(ctx,32,pose,cfg);
    out.drawImage(canvas,8,8,80,80,(yaw%18)*80,Math.floor(yaw/18)*80,80,80);
   }
   return sheet.toDataURL();
  },pitch);
  await png(join(assets,`pitch${String((pitch+45)/5).padStart(2,'0')}.png`),data);
 }
 const scenarios=await page.evaluate(()=>[.32,.71].map(seed=>{
  const sim=new BotReference.Sim(seed),frames=[];
  for(let i=0;i<1800;i++) {
   if(i===60)sim.setPointer(1,-.5,1);
   if(i===120)sim.poke();
   if(i===240)sim.setPointer(-.8,.6,1);
   if(i===360)sim.setPointer(0,0,0);
   if(i===480)sim.setState('working');
   if(i===720)sim.setState('sleeping');
   if(i===960)sim.setState('default');
   sim.update(1/60);
   if(i%6===0)frames.push({step:i+1,pose:structuredClone(sim.pose)});
  }
  return {seed,frames};
 }));
 await writeFile(join(fixtures,'motion.json'),JSON.stringify(scenarios));
 const samples=[{name:'rest',pose:null},...scenarios[0].frames.filter(f=>[91,139,169,253,505,793,985].includes(f.step)).map(f=>({name:'step-'+f.step,pose:f.pose}))];
 const rendered=await page.evaluate(samples=>{
  const cfg=BotReference.ghostConfig();const canvas=document.createElement('canvas');canvas.width=canvas.height=96;const ctx=canvas.getContext('2d');
  return samples.map(f=>{
   const pose=f.pose??BotReference.restPose('default');
   ctx.setTransform(2,0,0,2,0,0);BotReference.draw(ctx,32,pose,cfg);
   ctx.setTransform(1,0,0,1,0,0);ctx.globalCompositeOperation='destination-over';ctx.fillStyle='#ffffff';ctx.fillRect(0,0,96,96);ctx.globalCompositeOperation='source-over';
   return {name:f.name,pose,png:canvas.toDataURL()};
  });
 },samples);
 for(const f of rendered)await png(join(fixtures,f.name+'.png'),f.png);
 await writeFile(join(fixtures,'poses.json'),JSON.stringify(rendered.map(({name,pose})=>({name,pose}))));
 return {commit,materialViews:1152,motionPoses:600,referenceImages:rendered.length};
}
