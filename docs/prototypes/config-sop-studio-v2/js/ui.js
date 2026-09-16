/* UI primitives: escape, drawer, menu, toast, combobox popup, record form, download */
(function(){
  const esc=s=>String(s==null?'':s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const $=(s,r)=>(r||document).querySelector(s);
  const $$=(s,r)=>Array.from((r||document).querySelectorAll(s));
  const tag=(t,tone)=>`<span class="tag t-${tone||'mut'}">${esc(t)}</span>`;
  const statusTag=s=>s==='archived'?tag('Archived','mut'):s==='review'?tag('Review','warn'):tag('Active','ok');
  const fmtDate=iso=>{if(!iso)return '';const d=new Date(iso);if(isNaN(d))return esc(iso);const p=n=>String(n).padStart(2,'0');return p(d.getDate())+'/'+p(d.getMonth()+1)+'/'+d.getFullYear();};
  const fmtDateTime=iso=>{const d=new Date(iso);if(isNaN(d))return '';const p=n=>String(n).padStart(2,'0');return fmtDate(iso)+' '+p(d.getHours())+':'+p(d.getMinutes());};

  const UI={esc,$,$$,tag,statusTag,fmtDate,fmtDateTime,handlers:{}};

  /* ---------- drawer (single instance; never stacked) ---------- */
  UI.drawer=function(o){
    UI.closeMenu(); UI.closePop();
    let scrim=$('#scrim'),dr=$('#drawer');
    dr.className='drawer'+(o.wide?' wide':'');
    dr.innerHTML=`<div class="dh"><div><h2>${esc(o.title)}</h2>${o.sub?`<div class="sb">${esc(o.sub)}</div>`:''}</div><button class="x" data-a="drawer-close" aria-label="Close">${ic('x')}</button></div>
      <div class="dc">${o.body||''}</div>${o.foot?`<div class="df">${o.foot}</div>`:''}`;
    scrim.classList.remove('hidden');
    requestAnimationFrame(()=>dr.classList.add('on'));
    UI._drawer=o;
    if(o.mount)o.mount(dr);
  };
  UI.redrawDrawer=function(patch){if(!UI._drawer)return;const dc=$('#drawer .dc'),st=dc?dc.scrollTop:0;Object.assign(UI._drawer,patch||{});UI.drawer(UI._drawer);const n=$('#drawer .dc');if(n)n.scrollTop=st;};
  UI.closeDrawer=function(){const dr=$('#drawer');dr.classList.remove('on');$('#scrim').classList.add('hidden');UI.closePop();const o=UI._drawer;UI._drawer=null;if(o&&o.onClose)o.onClose();};

  /* ---------- menu ---------- */
  UI.menu=function(anchor,items){
    UI.closeMenu();
    const m=document.createElement('div'); m.className='menu'; m.id='menu';
    m.innerHTML=items.map((it,i)=>it==='-'?'<hr>':`<button data-mi="${i}" class="${it.danger?'dng':''}">${it.icon?ic(it.icon):''}${esc(it.label)}</button>`).join('');
    document.body.appendChild(m);
    const r=anchor.getBoundingClientRect(); const w=m.offsetWidth,hh=m.offsetHeight;
    let left=Math.min(r.right-w,window.innerWidth-w-8); left=Math.max(8,left);
    let top=r.bottom+4; if(top+hh>window.innerHeight-8)top=Math.max(8,r.top-hh-4);
    m.style.left=left+'px'; m.style.top=top+'px';
    m.addEventListener('click',e=>{const b=e.target.closest('[data-mi]');if(!b)return;const it=items[+b.dataset.mi];UI.closeMenu();it.run&&it.run();});
  };
  UI.closeMenu=function(){const m=$('#menu');if(m)m.remove();};

  /* ---------- toast ---------- */
  let tt;
  UI.toast=function(msg,undo,action){
    let t=$('#toast'); if(t)t.remove(); clearTimeout(tt);
    t=document.createElement('div'); t.className='toast'; t.id='toast'; t.setAttribute('role','status');
    t.innerHTML=`<span>${esc(msg)}</span>${action?`<button class="btn sm" data-act>${esc(action.label)}</button>`:''}${undo?`<button class="btn sm" data-undo>${ic('undo')}Undo</button>`:''}`;
    document.body.appendChild(t);
    if(undo)t.querySelector('[data-undo]').onclick=()=>{t.remove();undo();};
    if(action)t.querySelector('[data-act]').onclick=()=>{t.remove();action.run();};
    tt=setTimeout(()=>t.remove(),undo||action?7000:3200);
  };
  UI.undoable=function(label,fn){
    const n=S.snap(label); fn(); S.save();
    UI.toast(label,()=>{S.undoTo(n);App.render();UI.toast('Undone');});
  };
  UI.dupRow=function(regKey,rec){
    const reg=REG.R[regKey]; const v=REG.toRow(regKey,rec);
    reg.cols.forEach(c=>{if(c.unique)v[c.k]='';});
    const kc=reg.keyCol||(reg.cols.filter(c=>c.req&&c.type==='text').pop()||{}).k;
    if(kc&&v[kc]&&!reg.cols.find(c=>c.k===kc).unique)v[kc]=v[kc]+' copy';
    return v;
  };

  /* ---------- combobox popup ---------- */
  UI.pop=function(input,cfg){
    UI._popCfg={input,cfg,hi:0};
    let p=$('#cbpop'); if(!p){p=document.createElement('div');p.id='cbpop';p.className='cbpop';document.body.appendChild(p);
      p.addEventListener('mousedown',e=>{e.preventDefault();const o=e.target.closest('[data-oi]');if(o)UI._popChoose(+o.dataset.oi);});}
    UI._popRender();
  };
  UI._popItems=function(){
    const {input,cfg}=UI._popCfg; const q=(cfg.query?cfg.query():input.value).trim(); const nq=REG.norm(q);
    let opts=cfg.options(q)||[];
    const exact=opts.some(o=>REG.norm(o.label)===nq);
    if(nq)opts=opts.filter(o=>REG.norm(o.label+' '+(o.sub||'')).includes(nq)).concat(opts.filter(o=>!REG.norm(o.label+' '+(o.sub||'')).includes(nq)&&(o.alias||[]).some(a=>REG.norm(a)===nq)));
    opts=opts.slice(0,60);
    if(nq&&!exact){
      if(cfg.creates)opts=opts.concat(cfg.creates(q).map(c=>({create:true,label:c.label,run:c.run})));
      else if(cfg.onCreate)opts=opts.concat([{create:true,label:'Create “'+q+'”',text:q}]);
    }
    return opts;
  };
  UI._popRender=function(){
    const c=UI._popCfg; if(!c)return; const p=$('#cbpop'); if(!p)return;
    const items=UI._popItems(); c.items=items; if(c.hi>=items.length)c.hi=0;
    p.innerHTML=items.length?items.map((o,i)=>o.create?`<div class="o new ${i===c.hi?'hi':''}" data-oi="${i}">${ic('plus','',14)}${esc(o.label)}</div>`
      :`<div class="o ${i===c.hi?'hi':''}" data-oi="${i}">${esc(o.label)}${o.sub?`<small>${esc(o.sub)}</small>`:''}</div>`).join(''):'<div class="none">No matches</div>';
    const r=c.input.getBoundingClientRect();
    p.style.minWidth=Math.max(200,r.width)+'px';
    p.style.display='block';
    const ph=p.offsetHeight; let top=r.bottom+3; if(top+ph>window.innerHeight-6)top=Math.max(6,r.top-ph-3);
    p.style.left=Math.max(6,Math.min(r.left,window.innerWidth-p.offsetWidth-6))+'px'; p.style.top=top+'px';
  };
  UI._popChoose=function(i){
    const c=UI._popCfg; if(!c)return; const o=c.items[i]; if(!o)return;
    if(o.create){
      const text=(c.cfg.query?c.cfg.query():c.input.value).trim(); let res;
      if(o.run){S.snap('Created');const id=o.run();S.save();res={id,label:text};UI.toast('Created “'+text+'”');if(c.cfg.onCreated)c.cfg.onCreated(res);}
      else res=c.cfg.onCreate(o.text);
      if(!c.cfg.multi)c.input.value=res&&res.label?res.label:text;
      c.cfg.onPick&&c.cfg.onPick({id:res&&res.id,label:res&&res.label||text,created:true});
    }
    else{if(!c.cfg.multi)c.input.value=o.label; c.cfg.onPick&&c.cfg.onPick(o);}
    UI.closePop();
  };
  UI.popKey=function(e){
    const c=UI._popCfg; if(!c||!$('#cbpop')||$('#cbpop').style.display==='none')return false;
    if(e.key==='ArrowDown'){c.hi=Math.min(c.items.length-1,c.hi+1);UI._popRender();e.preventDefault();return true;}
    if(e.key==='ArrowUp'){c.hi=Math.max(0,c.hi-1);UI._popRender();e.preventDefault();return true;}
    if(e.key==='Enter'&&c.items.length){UI._popChoose(c.hi);e.preventDefault();return true;}
    if(e.key==='Escape'){UI.closePop();return true;}
    return false;
  };
  UI.closePop=function(){const p=$('#cbpop');if(p)p.style.display='none';UI._popCfg=null;};

  /* ref options for a register column in the context of a row */
  UI.refOptions=function(regKey,col,row){
    if(col.type==='enum'||col.type==='multienum')return col.opts.filter(Boolean).map(o=>({id:o,label:o}));
    if(col.type==='path')return S.active('categories').map(c=>({id:c.id,label:REG.catPath(c.id)}));
    const list=col.scope&&!String(row[col.scope.col]||'').trim()?S.active(col.ref):REG.candidates(regKey,Object.assign({},col,{type:'ref'}),row);
    return list.map(x=>({id:x.id,label:REG.label(col.ref,x),sub:subFor(col.ref,x),alias:x.aliases}));
  };
  function subFor(coll,x){
    if(coll==='pens'){const p=S.get('parks',x.parkId);return p?p.name:'';}
    if(coll==='partitions'){const p=S.get('pens',x.penId);return p?p.name:'';}
    if(['breeds','stages','sexes'].includes(coll)){const s=S.get('species',x.speciesId);return (x.code&&coll==='stages'?x.code+' · ':'')+(s?s.name:'');}
    if(coll==='parks'){const f=S.get('farms',x.farmId);return f?f.code:'';}
    return '';
  }

  /* ---------- record form (single row through the same validate/commit path) ---------- */
  UI.recordForm=function(regKey,recId,opts){
    opts=opts||{};
    const reg=REG.R[regKey]; const rec=recId?S.get(reg.coll,recId):null; const dup=!!opts.duplicate;
    const row={id:'f',v:rec?(dup?UI.dupRow(regKey,rec):REG.toRow(regKey,rec)):Object.assign({},opts.defaults||{}),recId:rec&&!dup?recId:null};
    if(!rec)reg.cols.forEach(c=>{if(row.v[c.k]===undefined)row.v[c.k]='';});
    const baseline=JSON.stringify(S.state); let created=false, saved=false, shown=false;
    const body=()=>{
      const res=REG.validate(regKey,[row],{createParents:true})[0];
      const st=reg.statuses.length>2||rec?`<div class="fld"><label>Status</label><select data-fstatus>${reg.statuses.map(x=>`<option value="${x}" ${(row.v.status||'active')===x?'selected':''}>${x[0].toUpperCase()+x.slice(1)}</option>`).join('')}</select></div>`:'';
      return `<div class="fgrid">${reg.cols.filter(c=>!c.virtual||c.scope||reg.cols.some(x=>x.scope&&x.scope.col===c.k)).map(c=>{
        const iss=shown&&res.issues[c.k]; const full=c.w==='wide'||c.type==='multi'||c.type==='multienum';
        const input=(c.type==='ref'||c.type==='path'||c.type==='enum'||c.type==='multi')
          ?`<input data-ff="${c.k}" value="${esc(row.v[c.k]||'')}" placeholder="${c.blankLabel?esc(c.blankLabel):'Search or create'}" autocomplete="off" aria-label="${esc(c.label)}">`
          :(c.type==='multienum'?`<div class="chkrow">${c.opts.map(o=>`<label><input type="checkbox" data-fm="${c.k}" value="${esc(o)}" ${String(row.v[c.k]||'').split(/;\s*/).includes(o)?'checked':''}>${esc(o)}</label>`).join('')}</div>`
          :`<input data-ff="${c.k}" value="${esc(row.v[c.k]||'')}" ${c.type==='num'?'inputmode="decimal"':''} ${c.type==='date'?'placeholder="DD/MM/YYYY"':''} aria-label="${esc(c.label)}">`);
        return `<div class="fld ${full?'full':''}"><label>${esc(c.label)}${c.req?' *':''}</label>${input}${iss?`<div class="ferr ${iss[0]==='err'?'':'w'}">${esc(iss[1])}</div>`:''}</div>`;
      }).join('')}${st}</div>`;
    };
    const save=()=>{
      shown=true;
      const res=REG.validate(regKey,[row],{createParents:true})[0];
      if(res.status==='error'){UI.redrawDrawer({body:body()});return;}
      if(res.status==='empty'){UI.closeDrawer();return;}
      const n=S.snap((rec&&!dup?'Saved ':'Added ')+reg.one.toLowerCase());
      REG.commit(regKey,[row],{createParents:true}); S.save(); saved=true;
      UI.closeDrawer(); App.render();
      UI.toast((rec&&!dup?'Saved':'Added')+' '+reg.one.toLowerCase(),()=>{S.undoTo(n);App.render();});
    };
    UI.drawer({title:(rec&&!dup?'Edit ':'New ')+reg.one.toLowerCase(),sub:rec&&!dup?REG.label(reg.coll,rec):'',body:body(),
      foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-rf-save>Save</button>`,
      onClose(){if(!saved&&created){S.state=JSON.parse(baseline);S.save();App.render();}},
      mount(dr){
        dr.querySelector('[data-rf-save]').onclick=save;
        const stSel=dr.querySelector('[data-fstatus]'); if(stSel)stSel.onchange=()=>{row.v.status=stSel.value;};
        dr.querySelectorAll('[data-fm]').forEach(cb=>cb.onchange=()=>{const k=cb.dataset.fm;row.v[k]=Array.from(dr.querySelectorAll(`[data-fm="${k}"]:checked`)).map(x=>x.value).join('; ');});
        dr.querySelectorAll('[data-ff]').forEach(inp=>{
          const c=reg.cols.find(x=>x.k===inp.dataset.ff);
          const isPick=c.type==='ref'||c.type==='path'||c.type==='enum'||c.type==='multi';
          inp.addEventListener('input',()=>{row.v[c.k]=inp.value; if(isPick)openPop();});
          inp.addEventListener('keydown',e=>{if(isPick&&UI.popKey(e))return; if(e.key==='Enter'&&!isPick)save();});
          inp.addEventListener('blur',()=>setTimeout(()=>{if(UI._popCfg&&UI._popCfg.input===inp)UI.closePop();},120));
          function openPop(){UI.pop(inp,UI.pickerCfg(regKey,c,row.v,inp,()=>{row.v[c.k]=inp.value;},()=>{created=true;}));}
          if(isPick)inp.addEventListener('focus',openPop);
        });
      }});
  };

  /* shared picker config for forms and grids: options + inline create (incl. multi) */
  UI.pickerCfg=function(regKey,c,rowV,inp,changed,onCreated){
    const multi=c.type==='multi';
    const last=()=>multi?inp.value.split(';').pop():inp.value;
    return {
      multi,query:last,
      options:()=>UI.refOptions(regKey,c,rowV),
      onPick(o){
        if(multi){const parts=inp.value.split(';').map(s=>s.trim());parts.pop();parts.push(o.label);inp.value=parts.filter(Boolean).join('; ');}
        changed&&changed();
      },
      creates:c.type==='enum'||c.type==='multienum'?null:(text)=>REG.createChoices(regKey,multi?Object.assign({},c,{type:'ref'}):c,rowV,text).map(x=>({label:x.label,run:()=>{onCreated&&onCreated();return x.run();}}))
    };
  };

  UI.download=function(name,content,mime){
    const blob=content instanceof Blob?content:new Blob([content],{type:mime||'text/csv;charset=utf-8'});
    const a=document.createElement('a'); a.href=URL.createObjectURL(blob); a.download=name; document.body.appendChild(a); a.click();
    setTimeout(()=>{URL.revokeObjectURL(a.href);a.remove();},500);
  };

  window.UI=UI; window.esc=esc;
})();
