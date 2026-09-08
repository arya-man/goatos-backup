import { chromium } from 'playwright';
import fs from 'fs';

const states = fs.readFileSync(process.argv[2],'utf8').split('\n').filter(Boolean);
const OUT = process.argv[3];
const START = +(process.argv[4]||0), END = +(process.argv[5]||states.length);

const probe = () => {
  const px = v => Math.round(parseFloat(v)*100)/100;
  const fam = f => { f=f.toLowerCase();
    if(f.includes('instrument serif'))return 'serif';
    if(f.includes('spline sans mono'))return 'mono';
    if(f.includes('instrument sans'))return 'sans';
    return 'other:'+f.split(',')[0]; };
  const out=[];
  const seen=new Set();
  const els=document.querySelectorAll('body *');
  for(const el of els){
    const r=el.getBoundingClientRect();
    if(r.width<1||r.height<1) continue;
    // direct text only
    let txt=''; for(const n of el.childNodes) if(n.nodeType===3) txt+=n.nodeValue;
    txt=txt.trim();
    if(!txt) continue;
    const cs=getComputedStyle(el);
    const classSig=(el.getAttribute('class')||'(none)')+'|'+el.tagName;
    const styleSig=[fam(cs.fontFamily),px(cs.fontSize),cs.fontWeight,cs.letterSpacing,cs.textTransform].join('/');
    const key=classSig+'::'+styleSig;
    if(seen.has(key))continue; seen.add(key);
    out.push({c:classSig,s:styleSig,color:cs.color,h:Math.round(r.height),t:txt.slice(0,30)});
  }
  // tab/chip strips: contrast
  const lum=c=>{const m=c.match(/[\d.]+/g).map(Number);const f=x=>{x/=255;return x<=.03928?x/12.92:Math.pow((x+.055)/1.055,2.4)};return .2126*f(m[0])+.7152*f(m[1])+.0722*f(m[2])};
  const eff=el=>{let e=el;while(e){const b=getComputedStyle(e).backgroundColor;const m=b.match(/[\d.]+/g);if(m&&(m.length<4||+m[3]>0.5))return b;e=e.parentElement}return 'rgb(0,0,0)'};
  const cr=(a,b)=>{const L1=lum(a),L2=lum(b);return Math.round(((Math.max(L1,L2)+.05)/(Math.min(L1,L2)+.05))*100)/100};
  const strips=[];
  for(const cont of document.querySelectorAll('*')){
    const kids=[...cont.children].filter(k=>/^(A|BUTTON)$/.test(k.tagName)&&k.textContent.trim());
    if(kids.length<2)continue;
    const on=kids.filter(k=>/\bon\b|\bactive\b|\bis-active\b/.test(k.className)||k.getAttribute('aria-selected')==='true'||k.getAttribute('aria-current'));
    if(!on.length||on.length===kids.length)continue;
    const off=kids.find(k=>!on.includes(k)); if(!off)continue;
    const a=on[0];
    strips.push({cont:cont.getAttribute('class')||cont.tagName, onC:a.getAttribute('class'), offC:off.getAttribute('class'),
      bgRatio:cr(eff(a),eff(off)), onTxt:cr(getComputedStyle(a).color,eff(a)), offTxt:cr(getComputedStyle(off).color,eff(off)),
      onH:Math.round(a.getBoundingClientRect().height), offH:Math.round(off.getBoundingClientRect().height)});
  }
  // control heights
  const hs={};
  for(const el of document.querySelectorAll('button,select,input,a.btn,a.chip,.chip,.btn,.seg>*')){
    const r=el.getBoundingClientRect(); if(r.height<1)continue;
    const k=(el.getAttribute('class')||el.tagName); (hs[k]=hs[k]||new Set()).add(Math.round(r.height));
  }
  const heights=Object.fromEntries(Object.entries(hs).map(([k,v])=>[k,[...v]]));
  // flex row misalignment
  const mis=[];
  for(const cont of document.querySelectorAll('*')){
    const cs=getComputedStyle(cont); if(cs.display!=='flex'||cs.flexDirection.startsWith('column'))continue;
    const kids=[...cont.children].filter(k=>k.getBoundingClientRect().height>0);
    if(kids.length<2)continue;
    const cy=kids.map(k=>{const r=k.getBoundingClientRect();return r.top+r.height/2});
    const d=Math.max(...cy)-Math.min(...cy);
    if(d>2&&Math.max(...kids.map(k=>k.getBoundingClientRect().height))<80)
      mis.push({cont:cont.getAttribute('class')||cont.tagName,delta:Math.round(d*10)/10,n:kids.length});
  }
  return {out,strips,heights,mis};
};

(async()=>{
  const b=await chromium.launch({channel:'chrome'});
  const ctx=await b.newContext({viewport:{width:1440,height:1000}});
  const p=await ctx.newPage();
  const results={};
  for(let i=START;i<END&&i<states.length;i++){
    const u=states[i];
    let ok=false;
    for(let a=0;a<6&&!ok;a++){
    try{
      await p.goto('http://127.0.0.1:3400'+u,{waitUntil:'networkidle',timeout:45000});
      await p.evaluate(()=>document.fonts.ready);
      await p.waitForTimeout(250);
      const dark=await p.evaluate(probe);
      await p.evaluate(()=>document.documentElement.classList.add('light'));
      await p.waitForTimeout(150);
      const light=await p.evaluate(probe);
      await p.evaluate(()=>document.documentElement.classList.remove('light'));
      results[u]={dark,light}; ok=true;
    }catch(e){ results[u]={err:String(e).slice(0,120)}; await p.waitForTimeout(20000); }
    }
    if(i%10===0){ fs.writeFileSync(OUT,JSON.stringify(results)); console.error('..'+i); }
  }
  fs.writeFileSync(OUT,JSON.stringify(results));
  await b.close();
})();
