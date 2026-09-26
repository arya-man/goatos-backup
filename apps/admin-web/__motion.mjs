import { chromium } from 'playwright';
const OUT='/private/tmp/claude-501/-Users-raviteja-mesha/043cba5f-5caf-4d15-9eaf-dc6fcb54d4df/scratchpad/redesign/story-audit/motion-charts';
const id=process.argv[2], label=process.argv[3];
const b=await chromium.launch();
const ctx=await b.newContext({viewport:{width:1440,height:900}});
const p=await ctx.newPage();
await p.goto(`http://127.0.0.1:6007/iframe.html?viewMode=story&id=${id}&globals=theme:dark`,{waitUntil:'load'});
await p.waitForTimeout(2000);
const box=async()=>p.evaluate(()=>{const e=document.querySelector('.kit-tabpanel');const i=document.querySelector('.kit-tab-ind');return {panel:e?e.getBoundingClientRect().height:null, panelOverflow:e?getComputedStyle(e).overflow:null, ind:i?i.getBoundingClientRect():null, docH:document.documentElement.scrollHeight, docW:document.documentElement.scrollWidth};});
console.log('before',JSON.stringify(await box()));
await p.screenshot({path:`${OUT}/seq-${label}-00-before.png`});
const tabs=await p.$$('[role=tab]');
await tabs[Math.min(2,tabs.length-1)].click();
for(let i=1;i<=6;i++){await p.waitForTimeout(60);await p.screenshot({path:`${OUT}/seq-${label}-${String(i).padStart(2,'0')}.png`});console.log(i*60+'ms',JSON.stringify(await box()));}
await p.waitForTimeout(900);
await p.screenshot({path:`${OUT}/seq-${label}-99-settled.png`});
console.log('settled',JSON.stringify(await box()));
await b.close();
