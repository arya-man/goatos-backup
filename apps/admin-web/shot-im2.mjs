import { chromium } from 'playwright';
const OUT='/private/tmp/claude-501/-Users-raviteja-mesha/043cba5f-5caf-4d15-9eaf-dc6fcb54d4df/scratchpad/redesign/story-audit/inputs-menus/';
const ids=process.argv.slice(2);
const b=await chromium.launch();
for(const vp of [[1440,900,'d'],[390,844,'m']]){
 for(const th of ['dark','light']){
  const ctx=await b.newContext({viewport:{width:vp[0],height:vp[1]}});
  const p=await ctx.newPage();
  for(const id of ids){
   try{
    await p.goto(`http://127.0.0.1:6007/iframe.html?viewMode=story&id=${id}&globals=theme:${th}`,{waitUntil:'load',timeout:20000});
    await p.waitForTimeout(900);
    await p.screenshot({path:`${OUT}${id}__${vp[2]}__${th}.png`,fullPage:true});
   }catch(e){console.log('ERR',id,vp[2],th,e.message);}
  }
  await ctx.close();
  console.log('pass done',vp[2],th);
 }
}
await b.close();
