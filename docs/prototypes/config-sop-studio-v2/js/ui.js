/* UI primitives: escape, drawer, menu, toast, combobox popup, record form, download */
(function(){
  const esc=s=>String(s==null?'':s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const $=(s,r)=>(r||document).querySelector(s);
  const $$=(s,r)=>Array.from((r||document).querySelectorAll(s));
  const tag=(t,tone)=>`<span class="tag t-${tone||'mut'}">${esc(t)}</span>`;
  const statusTag=s=>s==='archived'?tag('Archived','mut'):tag('Active','ok');
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
    m.innerHTML=items.map((it,i)=>it==='-'?'<hr>':`<button data-mi="${i}" class="${it.danger?'dng':''}" ${it.disabled?'disabled style="opacity:1;color:var(--muted);cursor:default"':''}>${it.icon?ic(it.icon):''}${esc(it.label)}</button>`).join('');
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
    t.innerHTML=`<span>${esc(msg)}</span>${action?(action.link?`<a href="javascript:void 0" data-act style="color:var(--brand);text-decoration:underline">${esc(action.label)}</a>`:`<button class="btn sm" data-act>${esc(action.label)}</button>`):''}${undo?`<button class="btn sm" data-undo>${ic('undo')}Undo</button>`:''}`;
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
    reg.cols.forEach(c=>{if(c.unique||c.idTag)v[c.k]='';});
    const kc=reg.keyCol||(reg.cols.filter(c=>c.req&&c.type==='text').pop()||{}).k;
    const kcol=kc&&reg.cols.find(c=>c.k===kc);
    if(kc&&v[kc]&&!kcol.unique&&!kcol.idTag)v[kc]=v[kc]+' copy';
    return v;
  };

  /* ---------- combobox popup ----------
     Existing values always come first (exact, starts-with, contains, alias).
     Enter picks the highlighted EXISTING value only. "+ Create" needs a click,
     or Ctrl/Cmd+Enter while it is highlighted. Nothing matching = nothing highlighted. */
  UI.pop=function(input,cfg){
    UI._popCfg={input,cfg,hi:-1,touched:false};
    let p=$('#cbpop'); if(!p){p=document.createElement('div');p.id='cbpop';p.className='cbpop';p.setAttribute('role','listbox');document.body.appendChild(p);
      p.addEventListener('mousedown',e=>{e.preventDefault();const o=e.target.closest('[data-oi]');if(o)UI._popChoose(+o.dataset.oi,true);});
      p.addEventListener('change',e=>{const x=e.target.closest('[data-popall]');if(x&&UI._popCfg)UI._popCfg.cfg.applyAll=x.checked;});}
    UI._popRender();
  };
  UI._popItems=function(){
    const {input,cfg}=UI._popCfg; const q=(cfg.query?cfg.query():input.value).trim(); const nq=REG.norm(q);
    const all=cfg.options(q)||[];
    const hay=o=>REG.norm(o.label+' '+(o.sub||''));
    let opts=all;
    UI._popCfg.noMatch=false;
    if(nq&&cfg.fuzzy){
      /* best match first: exact / contains, then word overlap, then letter-pair similarity */
      const pairs=s=>{const a=[];for(let i=0;i<s.length-1;i++)a.push(s.slice(i,i+2));return a;};
      const dice=(a,b)=>{const x=pairs(a),y=pairs(b);if(!x.length||!y.length)return 0;let m=0;const pool=y.slice();x.forEach(p=>{const i=pool.indexOf(p);if(i>=0){m++;pool.splice(i,1);}});return 2*m/(x.length+y.length);};
      const qw=nq.split(/[\s·>\-]+/).filter(Boolean);
      const score=o=>{const l=REG.norm(o.label);if(l===nq)return 3;let s=dice(nq,l);if(l.includes(nq)||nq.includes(l))s=Math.max(s,1.5);
        const lw=l.split(/[\s·>\-]+/);if(qw.some(w=>w.length>2&&lw.some(v=>v.startsWith(w)||w.startsWith(v)&&v.length>2)))s=Math.max(s,1);
        if((o.alias||[]).some(a=>REG.norm(a)===nq))s=Math.max(s,2);return s;};
      const ranked=all.map(o=>[score(o),o]).filter(x=>x[0]>=0.4).sort((a,b)=>b[0]-a[0]).map(x=>x[1]);
      if(ranked.length)opts=ranked;else UI._popCfg.noMatch=true;
    }
    else if(nq){
      const rank=o=>{const l=REG.norm(o.label);if(l===nq)return 0;if(l.startsWith(nq))return 1;if(hay(o).split(/[\s·>\-]+/).some(w=>w.startsWith(nq)))return 2;if(hay(o).includes(nq))return 3;if((o.alias||[]).some(a=>REG.norm(a)===nq))return 4;return 9;};
      opts=all.map(o=>[rank(o),o]).filter(x=>x[0]<9).sort((a,b)=>a[0]-b[0]).map(x=>x[1]);
    }
    opts=opts.slice(0,60);
    const exact=nq&&all.some(o=>REG.norm(o.label)===nq);
    if(nq&&!exact){
      if(cfg.creates)opts=opts.concat(cfg.creates(q).map(c=>({create:true,label:c.label,run:c.run})));
      else if(cfg.onCreate)opts=opts.concat([{create:true,label:'Create “'+q+'”',text:q}]);
    }
    return opts;
  };
  UI._popRender=function(){
    const c=UI._popCfg; if(!c)return; const p=$('#cbpop'); if(!p)return;
    const items=UI._popItems(); c.items=items;
    const firstExisting=items.findIndex(o=>!o.create);
    const q=(c.cfg.query?c.cfg.query():c.input.value).trim();
    if(!c.touched)c.hi=q&&!c.noMatch?firstExisting:-1;
    if(c.hi>=items.length)c.hi=items.length-1;
    const hasExisting=firstExisting>=0;
    p.innerHTML=(c.cfg.header?c.cfg.header():'')+(items.length?items.map((o,i)=>o.create?`<div class="o new ${i===c.hi?'hi':''}" data-oi="${i}" role="option">${ic('plus','',14)}${esc(o.label)}${i===c.hi?'<small>Click or Ctrl+Enter</small>':''}</div>`
      :`<div class="o ${i===c.hi?'hi':''}" data-oi="${i}" role="option" aria-selected="${i===c.hi}">${esc(o.label)}${o.sub?`<small>${esc(o.sub)}</small>`:''}</div>`).join(''):'')
      +((!hasExisting||c.noMatch)&&q?'<div class="none">No close match</div>':'')+(!items.length&&!q?'<div class="none">No values yet</div>':'');
    const r=c.input.getBoundingClientRect();
    p.style.minWidth=Math.max(200,r.width)+'px';
    p.style.display='block';
    const ph=p.offsetHeight; let top=r.bottom+3; if(top+ph>window.innerHeight-6)top=Math.max(6,r.top-ph-3);
    p.style.left=Math.max(6,Math.min(r.left,window.innerWidth-p.offsetWidth-6))+'px'; p.style.top=top+'px';
  };
  UI._popChoose=function(i,explicit){
    const c=UI._popCfg; if(!c)return; const o=c.items[i]; if(!o)return;
    if(o.create){
      if(!explicit)return;
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
    if(e.key==='ArrowDown'){c.touched=true;c.hi=Math.min(c.items.length-1,c.hi+1);UI._popRender();e.preventDefault();return true;}
    if(e.key==='ArrowUp'){c.touched=true;c.hi=Math.max(0,c.hi-1);UI._popRender();e.preventDefault();return true;}
    if(e.key==='Enter'){
      const o=c.items[c.hi];
      if(!o)return false;
      if(o.create){if(e.ctrlKey||e.metaKey){UI._popChoose(c.hi,true);e.preventDefault();return true;}e.preventDefault();return true;}
      UI._popChoose(c.hi);e.preventDefault();return true;
    }
    if(e.key==='Escape'){UI.closePop();return true;}
    return false;
  };
  UI.closePop=function(){const p=$('#cbpop');if(p)p.style.display='none';UI._popCfg=null;};

  /* ref options for a register column in the context of a row */
  UI.refOptions=function(regKey,col,row){
    if(col.type==='enum'||col.type==='multienum')return col.opts.filter(Boolean).map(o=>({id:o,label:REG.enumLabel(o)}));
    if(col.type==='steps')return REG.stepOptions();
    if(col.type==='path')return S.active('categories').map(c=>({id:c.id,label:REG.catPath(c.id)}));
    const list=col.scope&&!String(row[col.scope.col]||'').trim()?S.active(col.ref):REG.candidates(regKey,Object.assign({},col,{type:'ref'}),row);
    return list.map(x=>({id:x.id,label:REG.label(col.ref,x),sub:subFor(col.ref,x),alias:x.aliases}));
  };
  function subFor(coll,x){
    if(coll==='pens'){const p=S.get('parks',x.parkId);return p?p.name:'';}
    if(coll==='partitions'){const p=S.get('pens',x.penId);return p?REG.label('pens',p):'';}
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
      const res=REG.validate(regKey,[row],{})[0];
      const st=rec&&!dup?`<div class="fld"><label>Status</label><select data-fstatus>${reg.statuses.map(x=>`<option value="${x}" ${(row.v.status||'active')===x?'selected':''}>${REG.enumLabel(x)}</option>`).join('')}</select></div>`:'';
      return `<div class="fgrid">${reg.cols.filter(c=>!c.virtual||c.scope||reg.cols.some(x=>x.scope&&x.scope.col===c.k)).map(c=>{
        const iss=shown&&res.issues[c.k]; const full=c.w==='wide'||c.type==='multi'||c.type==='multienum';
        const input=c.type==='steps'?stepsField(c):(c.type==='ref'||c.type==='path'||c.type==='enum'||c.type==='multi')
          ?`<input data-ff="${c.k}" value="${esc(row.v[c.k]||'')}" placeholder="${c.blankLabel?esc(c.blankLabel):'Search or create'}" autocomplete="off" aria-label="${esc(c.label)}">`
          :(c.type==='multienum'?`<div class="chkrow">${c.opts.map(o=>`<label><input type="checkbox" data-fm="${c.k}" value="${esc(REG.enumLabel(o))}" ${String(row.v[c.k]||'').split(/;\s*/).map(REG.norm).includes(REG.norm(REG.enumLabel(o)))?'checked':''}>${esc(REG.enumLabel(o))}</label>`).join('')}</div>`
          :`<input data-ff="${c.k}" value="${esc(row.v[c.k]||'')}" ${c.type==='num'?'inputmode="decimal"':''} ${c.type==='date'?'placeholder="YYYY-MM-DD"':''} aria-label="${esc(c.label)}">`);
        return `<div class="fld ${full?'full':''}"><label>${esc(c.label)}${c.req?' *':''}</label>${input}${iss?`<div class="ferr ${iss[0]==='err'?'':'w'}">${esc(iss[1])}${iss[2]&&iss[2].create?` <button type="button" class="btn sm" data-rf-create="${c.k}">${ic('plus','',12)}Create “${esc(iss[2].text)}”</button>`:''}</div>`:''}</div>`;
      }).join('')}${st}</div>`;
    };
    /* ordered approver chips picked from Roles / Designations / People; stored as "A; B" like the grid */
    const stepList=k=>String(row.v[k]||'').split(';').map(x=>x.trim()).filter(Boolean);
    const stepsField=c=>{const list=stepList(c.k);
      return `<div class="stepchips" style="display:flex;flex-wrap:wrap;gap:6px;align-items:center">${list.map((t,i)=>`<span class="tag t-mut" style="display:inline-flex;align-items:center;gap:4px;padding-right:2px"><b>${i+1}.</b> ${esc(t)}
        ${i?`<button type="button" class="btn icon gh sm" data-step-up="${c.k}" data-i="${i}" aria-label="Move ${esc(t)} earlier" style="width:22px;height:22px;min-height:0"><span style="display:inline-flex;transform:rotate(180deg)">${ic('arrow-down','',12)}</span></button>`:''}
        <button type="button" class="btn icon gh sm" data-step-rm="${c.k}" data-i="${i}" aria-label="Remove ${esc(t)}" style="width:22px;height:22px;min-height:0">${ic('x','',12)}</button></span>`).join('')}
        <input data-steps-add="${c.k}" placeholder="${list.length?'Add next approver':'Add approver: role, designation or person'}" autocomplete="off" aria-label="Add approver" style="flex:1;min-width:180px"></div>`;};
    const save=()=>{
      shown=true;
      const res=REG.validate(regKey,[row],{})[0];
      if(res.status==='error'){UI.redrawDrawer({body:body()});return;}
      if(res.status==='empty'){UI.closeDrawer();return;}
      const n=S.snap((rec&&!dup?'Saved ':'Added ')+reg.one.toLowerCase());
      REG.commit(regKey,[row],{}); S.save(); saved=true;
      UI.closeDrawer(); App.render();
      UI.toast((rec&&!dup?'Saved':'Added')+' '+reg.one.toLowerCase(),()=>{S.undoTo(n);App.render();});
    };
    UI.drawer({title:(rec&&!dup?'Edit ':'New ')+reg.one.toLowerCase(),sub:rec&&!dup?REG.label(reg.coll,rec):'',body:body(),
      foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-rf-save>Save</button>`,
      onClose(){if(!saved&&created){S.state=JSON.parse(baseline);S.save();App.render();}},
      mount(dr){
        dr.querySelector('[data-rf-save]').onclick=save;
        dr.querySelectorAll('[data-rf-create]').forEach(b=>b.onclick=e=>{const c=reg.cols.find(x=>x.k===b.dataset.rfCreate);const ch=REG.createChoices(regKey,c.type==='multi'?Object.assign({},c,{type:'ref'}):c,row.v,(REG.validate(regKey,[row],{})[0].issues[c.k][2]||{}).text);
          const doIt=x=>{S.snap('Created');x.run();created=true;S.save();UI.redrawDrawer({body:body()});};
          if(ch.length===1)doIt(ch[0]);else UI.menu(b,ch.map(x=>({label:x.label,icon:'plus',run:()=>doIt(x)})));});
        const redraw=k=>{UI.redrawDrawer({body:body()});const n=$(`#drawer [data-steps-add="${k}"]`);if(n)n.focus();};
        dr.querySelectorAll('[data-step-rm]').forEach(b=>b.onclick=()=>{const k=b.dataset.stepRm,l=stepList(k);l.splice(+b.dataset.i,1);row.v[k]=l.join('; ');redraw(k);});
        dr.querySelectorAll('[data-step-up]').forEach(b=>b.onclick=()=>{const k=b.dataset.stepUp,l=stepList(k),i=+b.dataset.i;[l[i-1],l[i]]=[l[i],l[i-1]];row.v[k]=l.join('; ');redraw(k);});
        dr.querySelectorAll('[data-steps-add]').forEach(inp=>{const k=inp.dataset.stepsAdd;
          const open=()=>UI.pop(inp,{options:()=>{const have=stepList(k).map(x=>REG.norm(x.replace(/\(per person\)\s*$/i,'')));return REG.stepOptions().filter(o=>!have.includes(REG.norm(o.label)));},
            onPick(o){row.v[k]=stepList(k).concat([o.label]).join('; ');setTimeout(()=>redraw(k),0);}});
          inp.addEventListener('focus',open);inp.addEventListener('input',open);
          inp.addEventListener('keydown',e=>{if(UI.popKey(e))return;if(e.key==='Backspace'&&!inp.value){const l=stepList(k);if(l.length){l.pop();row.v[k]=l.join('; ');redraw(k);}}});
          inp.addEventListener('blur',()=>setTimeout(()=>{if(UI._popCfg&&UI._popCfg.input===inp)UI.closePop();},120));});
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
    const multi=c.type==='multi'||c.type==='steps';
    const last=()=>multi?inp.value.split(';').pop():inp.value;
    return {
      multi,query:last,
      options:()=>UI.refOptions(regKey,c,rowV),
      onPick(o){
        if(multi){const parts=inp.value.split(';').map(s=>s.trim());parts.pop();parts.push(o.label);inp.value=parts.filter(Boolean).join('; ');}
        changed&&changed();
      },
      creates:c.type==='enum'||c.type==='multienum'||c.type==='steps'?null:(text)=>REG.createChoices(regKey,multi?Object.assign({},c,{type:'ref'}):c,rowV,text).map(x=>({label:x.label,run:()=>{onCreated&&onCreated();return x.run();}}))
    };
  };

  UI.download=function(name,content,mime){
    const blob=content instanceof Blob?content:new Blob([content],{type:mime||'text/csv;charset=utf-8'});
    const a=document.createElement('a'); a.href=URL.createObjectURL(blob); a.download=name; document.body.appendChild(a); a.click();
    setTimeout(()=>{URL.revokeObjectURL(a.href);a.remove();},500);
  };

  window.UI=UI; window.esc=esc;
})();
