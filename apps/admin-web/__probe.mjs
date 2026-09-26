import { chromium } from 'playwright';
const b=await chromium.launch();const ctx=await b.newContext({viewport:{width:1440,height:900}});const p=await ctx.newPage();
p.on('pageerror',e=>console.log('PAGEERROR',e.message.slice(0,200)));
for(const th of ['light','dark']){
 await p.goto(`http://127.0.0.1:6007/iframe.html?viewMode=story&id=kit-pageenter--many-items&globals=theme:${th}`,{waitUntil:'load'});
 await p.waitForTimeout(3500);
 console.log(th, JSON.stringify(await p.evaluate(()=>{const r=document.querySelector('#storybook-root');const it=document.querySelectorAll('.kit-enter-item');return {kids:r?r.children.length:-1,items:it.length,op:it[0]?getComputedStyle(it[0]).opacity:null,htmlClass:document.documentElement.className,bodyBg:getComputedStyle(document.body).backgroundColor};})));
}
await b.close();
