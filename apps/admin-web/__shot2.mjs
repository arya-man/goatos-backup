import { chromium } from 'playwright';
const OUT='/private/tmp/claude-501/-Users-raviteja-mesha/043cba5f-5caf-4d15-9eaf-dc6fcb54d4df/scratchpad/redesign/story-audit/motion-charts';
const ids=process.argv.slice(2);
const b=await chromium.launch();
for(const vp of [{n:'d',w:1440,h:900},{n:'m',w:390,h:844}]){
 for(const th of ['dark','light']){
  const ctx=await b.newContext({viewport:{width:vp.w,height:vp.h}});
  const p=await ctx.newPage();
  for(const id of ids){
   try{
    await p.goto(`http://127.0.0.1:6007/iframe.html?viewMode=story&id=${id}&globals=theme:${th}`,{waitUntil:'load',timeout:40000});
    await p.waitForFunction(()=>{const r=document.querySelector('#storybook-root');return r&&r.children.length>0;},{timeout:30000}).catch(()=>{});
    await p.waitForTimeout(2500);
    const m=await p.evaluate(()=>({w:document.documentElement.scrollWidth,h:document.documentElement.scrollHeight,vw:innerWidth}));
    if(m.w>m.vw+1) console.log('HSCROLL',id,vp.n,th,m.w,'>',m.vw);
    await p.screenshot({path:`${OUT}/${id}__${vp.n}__${th}.png`,fullPage:true});
   }catch(e){console.log('ERR',id,vp.n,th,e.message.slice(0,60));}
  }
  await ctx.close();
 }
}
await b.close();console.log('ok');
