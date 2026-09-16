/* Generic register list: search, filters, status, selection, bulk actions, row menu */
(function(){
  window.A=window.A||{};
  const L={st:{}};
  const state=k=>L.st[k]||(L.st[k]={q:'',f:{},status:'active',sel:new Set()});

  function rows(regKey,opts){
    const reg=REG.R[regKey], s=state(regKey);
    let list=S.all(reg.coll);
    if(opts&&opts.where)list=list.filter(opts.where);
    if(s.status==='active')list=list.filter(r=>r.status!=='archived');
    else if(s.status==='archived')list=list.filter(r=>r.status==='archived');
    Object.entries(s.f).forEach(([k,val])=>{if(!val)return;const c=reg.cols.find(x=>x.k===k);if(!c)return;
      list=list.filter(r=>{const v=c.derive?c.derive(r):r[c.attr];return Array.isArray(v)?v.includes(val):String(v)===String(val);});});
    const q=REG.norm(s.q);
    if(q)list=list.filter(r=>REG.norm(Object.values(REG.toRow(regKey,r)).join(' ')).includes(q));
    return list;
  }
  L.rows=rows;

  L.html=function(regKey,opts){
    opts=opts||{}; const reg=REG.R[regKey], s=state(regKey);
    const list=rows(regKey,opts);
    const all=S.all(reg.coll).filter(opts.where||(()=>true));
    const fcols=(opts.filters||[]).map(k=>reg.cols.find(c=>c.k===k)).filter(Boolean);
    const selN=[...s.sel].filter(id=>all.some(r=>r.id===id)).length;
    return `<section class="card" data-list="${regKey}">
      <div class="hd lhd"><div class="ttl">${opts.eyebrow?`<div class="eyebrow">${esc(opts.eyebrow)}</div>`:''}<h3>${esc(opts.title||reg.label)} <span class="cnt">${all.filter(r=>r.status!=='archived').length}</span></h3></div><span class="sp"></span>
        <button class="btn sm" data-a="io-menu" data-reg="${regKey}">${ic('sheet')}Import / export</button>
        ${opts.extraBtns||''}
        ${opts.addAct?opts.addAct:`<button class="btn sm p" data-a="rec-new" data-reg="${regKey}">${ic('plus')}${esc(opts.addLabel||'Add '+reg.one.toLowerCase())}</button>`}
      </div>
      <div class="lbar">
        <label class="search">${ic('search')}<input data-lsearch="${regKey}" value="${esc(s.q)}" placeholder="Search ${esc(reg.label.toLowerCase())}"></label>
        ${fcols.map(c=>`<select class="fsel" data-lfilter="${regKey}" data-col="${c.k}" aria-label="${esc(c.label)}"><option value="">${esc(c.label)}: all</option>${
          (c.type==='enum'||c.type==='multienum'?c.opts.filter(Boolean).map(o=>({id:o,label:o})):S.active(c.ref).filter(x=>!(c.scope&&s.f[c.scope.col])||x[c.scope.attr]===s.f[c.scope.col]).map(x=>({id:x.id,label:REG.fullLabel(c.ref,x)})))
          .map(o=>`<option value="${esc(o.id)}" ${s.f[c.k]===o.id?'selected':''}>${esc(o.label)}</option>`).join('')}</select>`).join('')}
        <span class="sp"></span>
        <div class="seg">${['active','archived','all'].map(x=>`<button class="${s.status===x?'on':''}" data-a="lstatus" data-reg="${regKey}" data-v="${x}">${x[0].toUpperCase()+x.slice(1)}</button>`).join('')}</div>
      </div>
      ${selN?`<div class="bulkbar"><span>${selN} selected</span><span class="sp"></span>
        <button class="btn sm" data-a="bulk-grid" data-reg="${regKey}">${ic('sheet')}Edit in grid</button>
        <button class="btn sm" data-a="bulk-dup" data-reg="${regKey}">${ic('copy')}Duplicate</button>
        <button class="btn sm" data-a="bulk-status" data-reg="${regKey}" data-v="archived">${ic('archive')}Archive</button>
        <button class="btn sm" data-a="bulk-status" data-reg="${regKey}" data-v="active">${ic('rotate')}Restore</button>
        <button class="btn sm" data-a="bulk-export" data-reg="${regKey}">${ic('download')}Export</button>
        <button class="btn sm dngo" data-a="bulk-del" data-reg="${regKey}">${ic('trash')}Delete</button>
        <button class="btn sm gh" data-a="bulk-clear" data-reg="${regKey}">Clear</button></div>`:''}
      <div class="twrap screen ltable" data-ltable="${regKey}">${L.table(regKey,opts,list)}</div>
    </section>`;
  };

  L.table=function(regKey,opts,list){
    const reg=REG.R[regKey], s=state(regKey); list=list||rows(regKey,opts);
    const firstText=(reg.cols.find(c=>c.type==='text'&&c.req)||reg.cols[0]).k;
    const cols=opts.columns||reg.cols.filter(c=>!c.hideInList).map(c=>({label:c.label,num:c.type==='num',w:c.type==='num'?110:null,html:r=>{const v=REG.cellValue(reg,c,r);return c.k===firstText?`<b>${esc(v)}</b>`:`<span class="${c.type==='ref'&&!v?'muted':''}">${esc(v||(c.blankLabel||''))}</span>`;}}));
    if(!list.length)return `<div class="empty">${S.all(reg.coll).length?'No matches':'No '+esc(reg.label.toLowerCase())+' yet'}</div>`;
    const shown=list.slice(0,500);
    const allSel=shown.every(r=>s.sel.has(r.id));
    return `<table class="ltbl"><colgroup><col style="width:44px">${cols.map(c=>`<col${c.w?` style="width:${c.w}px"`:''}>`).join('')}<col style="width:104px"><col style="width:52px"></colgroup>
      <thead><tr><th class="ck"><input type="checkbox" data-a="sel-all" data-reg="${regKey}" ${allSel?'checked':''} aria-label="Select all"></th>${cols.map(c=>`<th class="${c.num?'num':''}">${esc(c.label)}</th>`).join('')}<th>Status</th><th></th></tr></thead>
      <tbody>${shown.map(r=>`<tr class="clk ${r.status==='archived'?'arch':''}" data-a="row-open" data-reg="${regKey}" data-id="${r.id}">
        <td class="ck"><input type="checkbox" data-a="sel-one" data-reg="${regKey}" data-id="${r.id}" ${s.sel.has(r.id)?'checked':''} aria-label="Select"></td>
        ${cols.map(c=>`<td class="${c.num?'num':''}">${c.html(r)}</td>`).join('')}<td>${UI.statusTag(r.status)}</td>
        <td class="act"><button class="btn icon gh" data-a="row-menu" data-reg="${regKey}" data-id="${r.id}" aria-label="Row actions">${ic('more')}</button></td></tr>`).join('')}</tbody></table>
      ${list.length>500?`<div class="empty">${shown.length} of ${list.length}</div>`:''}`;
  };

  L.opts={}; // regKey -> opts used on current screen (set by page renderers)
  L.refreshTable=function(regKey){const el=document.querySelector(`[data-ltable="${regKey}"]`);if(el)el.innerHTML=L.table(regKey,L.opts[regKey]||{});};

  /* actions */
  Object.assign(A,{
    'lstatus'(el){state(el.dataset.reg).status=el.dataset.v;App.render();},
    'sel-all'(el,e){e.stopPropagation();const k=el.dataset.reg,s=state(k);const list=rows(k,L.opts[k]).slice(0,500);if(el.checked)list.forEach(r=>s.sel.add(r.id));else list.forEach(r=>s.sel.delete(r.id));App.render();},
    'sel-one'(el,e){e.stopPropagation();const s=state(el.dataset.reg);el.checked?s.sel.add(el.dataset.id):s.sel.delete(el.dataset.id);App.render();},
    'bulk-clear'(el){state(el.dataset.reg).sel.clear();App.render();},
    'row-open'(el,e){if(e.target.closest('[data-a="row-menu"],input'))return;const k=el.dataset.reg;const o=L.opts[k]||{};if(o.onOpen)return o.onOpen(el.dataset.id);UI.recordForm(k,el.dataset.id);},
    'rec-new'(el){const k=el.dataset.reg;const o=L.opts[k]||{};if(o.onNew)return o.onNew();UI.recordForm(k,null,{defaults:o.defaults&&o.defaults()});},
    'grid-add'(el){const k=el.dataset.reg;const o=L.opts[k]||{};Sheet.openEntry(k,null,[o.defaults?o.defaults():{}]);},
    'row-menu'(el,e){
      e.stopPropagation(); const k=el.dataset.reg,id=el.dataset.id,reg=REG.R[k],rec=S.get(reg.coll,id),o=L.opts[k]||{};
      UI.menu(el,[
        {label:'Edit',icon:'edit-3',run:()=>o.onOpen?o.onOpen(id):UI.recordForm(k,id)},
        {label:'Edit in grid',icon:'sheet',run:()=>Sheet.openEntry(k,[id])},
        {label:'Duplicate',icon:'copy',run:()=>UI.recordForm(k,id,{duplicate:true})},
        rec.status==='archived'?{label:'Restore',icon:'rotate',run:()=>UI.undoable('Restored',()=>{rec.status='active';App.render();})}
          :{label:'Archive',icon:'archive',run:()=>UI.undoable('Archived',()=>{rec.status='archived';App.render();})},
        '-',
        {label:'Delete',icon:'trash',danger:true,run:()=>{const u=REG.usage(reg.coll,id);if(u.length){UI.toast('In use · '+u.map(x=>x.n+' '+x.label.toLowerCase()).join(', '),null,{label:'Archive',run:()=>UI.undoable('Archived',()=>{rec.status='archived';App.render();})});return;}
          UI.undoable('Deleted',()=>{S.remove(reg.coll,id);state(k).sel.delete(id);App.render();});}}
      ]);
    },
    'bulk-status'(el){const k=el.dataset.reg,s=state(k),reg=REG.R[k];const ids=[...s.sel];UI.undoable((el.dataset.v==='archived'?'Archived ':'Restored ')+ids.length,()=>{ids.forEach(id=>{const r=S.get(reg.coll,id);if(r)r.status=el.dataset.v;});s.sel.clear();App.render();});},
    'bulk-del'(el){const k=el.dataset.reg,s=state(k),reg=REG.R[k];const ids=[...s.sel];const used=ids.filter(id=>REG.usage(reg.coll,id).length);const free=ids.filter(id=>!used.includes(id));
      if(!free.length){UI.toast(used.length+' in use · archive instead');return;}
      UI.undoable('Deleted '+free.length+(used.length?' · '+used.length+' in use kept':''),()=>{S.remove(reg.coll,free);free.forEach(id=>s.sel.delete(id));App.render();});},
    'bulk-dup'(el){const k=el.dataset.reg,s=state(k),reg=REG.R[k];const ids=[...s.sel];
      Sheet.openEntry(k,null,ids.map(id=>UI.dupRow(k,S.get(reg.coll,id))));},
    'bulk-grid'(el){const k=el.dataset.reg;Sheet.openEntry(k,[...state(k).sel]);},
    'bulk-export'(el){const k=el.dataset.reg;IO.exportRegister(k,'csv',[...state(k).sel]);},
    'io-menu'(el){const k=el.dataset.reg;UI.menu(el,[
      {label:'Add in grid',icon:'sheet',run:()=>{const o=L.opts[k]||{};Sheet.openEntry(k,null,[o.defaults?o.defaults():{}]);}},
      {label:'Import file',icon:'upload',run:()=>IO.start(k)},
      '-',
      {label:'Template (.xlsx)',icon:'download',run:()=>IO.template(k,'xlsx')},
      {label:'Template (.csv)',icon:'download',run:()=>IO.template(k,'csv')},
      '-',
      {label:'Export (.xlsx)',icon:'download',run:()=>IO.exportRegister(k,'xlsx')},
      {label:'Export (.csv)',icon:'download',run:()=>IO.exportRegister(k,'csv')}]);}
  });

  document.addEventListener('input',e=>{
    const s=e.target.closest('[data-lsearch]'); if(s){state(s.dataset.lsearch).q=s.value;L.refreshTable(s.dataset.lsearch);}
  });
  document.addEventListener('change',e=>{
    const f=e.target.closest('[data-lfilter]'); if(f){state(f.dataset.lfilter).f[f.dataset.col]=f.value;App.render();}
  });
  window.List=L;
})();
