import { chromium } from 'playwright';
const b = await chromium.launch({channel:'chrome'});
const p = await b.newPage({ viewport:{width:1440,height:1000} });
const urls = ['/operations/audit?scope_mode=company','/protocol-adherence?scope_mode=company&adh_page=1&adh_limit=10','/procurement/source-entry?scope_mode=company','/people?scope_mode=company'];
for (const u of urls){
  await p.goto('http://127.0.0.1:3400'+u,{waitUntil:'networkidle',timeout:60000});
  await p.evaluate(()=>document.fonts.ready);
  await p.waitForTimeout(800);
  const r = await p.evaluate(()=>{
    const m=new Map();
    document.querySelectorAll('body *').forEach(e=>{
      const cl=(e.getAttribute('class')||'').trim(); if(!cl) return;
      const rect=e.getBoundingClientRect(); if(rect.width<1||rect.height<1) return;
      const key=e.tagName.toLowerCase()+'.'+cl;
      if(!m.has(key)) m.set(key,{n:0,txt:(e.textContent||'').trim().slice(0,40)});
      m.get(key).n++;
    });
    return [...m.entries()].map(([k,v])=>k+' ×'+v.n+' | '+v.txt);
  });
  console.log('\n===== '+u+' ('+r.length+' distinct) =====');
  console.log(r.join('\n'));
}
await b.close();
