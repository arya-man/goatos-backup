/* Generic register list: search, filters, status, selection, bulk actions, row menu */
(function(){
  window.A=window.A||{};
  const L={st:{}};
  const state=k=>L.st[k]||(L.st[k]={q:'',f:{},status:'active',sel:new Set(),anchor:null,showKeys:!KEYS_HIDDEN.includes(k)});
  /* raw keys are noise on these lists: hidden until "Show keys" in the Import / export menu */
  const KEYS_HIDDEN=['designations','roles','identifierPolicies'];
  /* phone width card layout is styled by app.css from td[data-label] */

  function rows(regKey,opts){
    const reg=REG.R[regKey], s=state(regKey);
    let list=S.all(reg.coll);
    if(opts&&opts.where)list=list.filter(opts.where);
    if(s.only){const set=new Set(s.only.ids);list=list.filter(r=>set.has(r.id));}
    if(s.status==='active')list=list.filter(r=>r.status!=='archived');
    else if(s.status==='archived')list=list.filter(r=>r.status==='archived');
    Object.entries(s.f).forEach(([k,val])=>{if(!val)return;const c=reg.cols.find(x=>x.k===k);if(!c)return;
      list=list.filter(r=>{const v=c.derive?c.derive(r):r[c.attr];return Array.isArray(v)?v.includes(val):String(v)===String(val);});});
    const q=REG.norm(s.q);
    if(q)list=list.filter(r=>REG.norm(Object.values(REG.toRow(regKey,r)).join(' ')).includes(q));
    if(opts&&opts.sort)list=list.slice().sort(opts.sort);
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
        ${opts.addAct?opts.addAct:`<button class="btn sm p" data-a="rec-new" data-reg="${regKey}">${ic('plus')}${esc(String(opts.addLabel||'Add '+reg.one.toLowerCase()).replace(/^\+\s*/,''))}</button>`}
      </div>
      <div class="lbar">
        <label class="search">${ic('search')}<input data-lsearch="${regKey}" value="${esc(s.q)}" placeholder="Search ${esc(reg.label.toLowerCase())}"></label>
        ${fcols.map(c=>`<select class="fsel" data-lfilter="${regKey}" data-col="${c.k}" aria-label="${esc(c.label)}"><option value="">${esc(c.label)}: all</option>${
          (c.type==='enum'||c.type==='multienum'?c.opts.filter(Boolean).map(o=>({id:o,label:REG.enumLabel(o)})):S.active(c.ref).filter(x=>!(c.scope&&s.f[c.scope.col])||x[c.scope.attr]===s.f[c.scope.col]).map(x=>({id:x.id,label:REG.fullLabel(c.ref,x)})))
          .map(o=>`<option value="${esc(o.id)}" ${s.f[c.k]===o.id?'selected':''}>${esc(o.label)}</option>`).join('')}</select>`).join('')}
        ${s.only?`<button class="chip on" data-a="lonly-clear" data-reg="${regKey}" aria-label="Clear filter">${esc(s.only.label)}&nbsp;×</button>`:''}
        <span class="sp"></span>
        <div class="seg">${['active','archived','all'].map(x=>`<button class="${s.status===x?'on':''}" data-a="lstatus" data-reg="${regKey}" data-v="${x}">${x[0].toUpperCase()+x.slice(1)}</button>`).join('')}</div>
      </div>
      ${selN?`<div class="bulkbar"><span>${selN} selected</span><span class="sp"></span>
        ${regKey==='animals'?`<button class="btn sm" data-a="bulk-move" data-reg="${regKey}">${ic('map-pin')}Move to pen</button>
        <button class="btn sm" data-a="bulk-stage" data-reg="${regKey}">${ic('layers')}Set stage</button>
        ${s.only&&['breeds','sexes'].includes(s.only.coll)?`<button class="btn sm" data-a="bulk-set" data-reg="${regKey}" data-coll="${s.only.coll}">${ic('layers')}Set ${esc(REG.R[s.only.coll].one.toLowerCase())}</button>`:''}
        <button class="btn sm" data-a="bulk-tag" data-reg="${regKey}">${ic('plus')}Add tag</button>`:''}
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
    /* human name first; a raw key (column "Key", or a record's key/code) goes last, muted, hidden on phones */
    const isKeyCol=c=>/^key$/i.test(c.label);
    const nameCol=reg.cols.find(c=>c.k==='name'&&!isKeyCol(c))||reg.cols.find(c=>c.type==='text'&&c.req&&!isKeyCol(c))||reg.cols[0];
    const firstText=nameCol.k;
    const ordered=[nameCol].concat(reg.cols.filter(c=>c!==nameCol&&!isKeyCol(c)),reg.cols.filter(c=>c!==nameCol&&isKeyCol(c))).filter(c=>!c.hideInList);
    const gradeCell=(c,r)=>c.k==='grade'?REG.gradeLabel(r.grade):REG.cellValue(reg,c,r);
    let cols=(regKey==='items'?ITEM_COLS:null)||opts.columns||ordered.map(c=>isKeyCol(c)?{label:'Key',key:1,w:150,html:r=>`<span class="mono muted">${esc(REG.cellValue(reg,c,r))}</span>`}
      :{label:c.label,num:c.type==='num',w:c.type==='num'?110:null,html:r=>{const v=gradeCell(c,r);if(c.ref==='pens'){const pn=S.get('pens',r[c.attr]);if(pn&&pn.displayName)return esc(pn.displayName);}if(regKey==='pens'&&c.k===firstText&&r.displayName)return `<b>${esc(r.displayName)}</b>`;return c.k===firstText?`<b>${esc(v)}</b>`:`<span class="${c.type==='ref'&&!v?'muted':''}">${esc(v||(c.blankLabel||''))}</span>`;}});
    if(regKey!=='items'&&!cols.some(c=>c.key||/^(key|code)$/i.test(c.label))&&S.all(reg.coll).some(r=>r.key))
      cols=cols.concat([{label:'Key',key:1,w:170,html:r=>`<span class="mono muted">${esc(r.key||'')}</span>`}]);
    if(!s.showKeys)cols=cols.filter(c=>!(c.key||/^(key|code)$/i.test(c.label)));
    if(regKey==='items'&&!list.some(r=>L.itemChips(r)))cols=cols.filter(c=>c.label!=='Tracking');
    cols=cols.map(c=>c.low?Object.assign({},c,{cls:'lowpri'}):c);
    cols=cols.map(c=>c.key?Object.assign({},c,{cls:'keycol'}):/^grade$/i.test(c.label)?Object.assign({},c,{html:r=>esc(REG.gradeLabel(r.grade))}):c);
    /* count columns that are zero on every row say nothing: drop them */
    cols=cols.filter(c=>!c.num||!list.length||list.slice(0,500).some(r=>{const v=String(c.html(r)).replace(/<[^>]*>/g,'').trim();return v!==''&&v!=='0'&&v!=='—';}));
    if(!list.length){const scoped=S.all(reg.coll).filter(opts.where||(()=>true));
      return scoped.length?`<div class="empty">No matches</div>`:`<div class="empty">No ${esc(reg.label.toLowerCase())} yet${regKey==='items'?`<div style="margin-top:10px"><button class="btn sm p" data-a="rec-new" data-reg="items">${ic('plus')}Add item</button></div>`:''}</div>`;}
    const shown=list.slice(0,500);
    /* widths by content: tables scroll inside .twrap instead of truncating */
    const MINW={rfid:170,'second tag':130,stage:190,'lifecycle stage':190,park:140,pen:140,partition:150,breed:150,species:110,name:180};
    const widthOf=c=>c.w||MINW[String(c.label).toLowerCase()]||(c.num?110:130);
    const plain=h=>String(h).replace(/<[^>]*>/g,'').replace(/&amp;/g,'&').replace(/&lt;/g,'<').replace(/&gt;/g,'>').replace(/&quot;/g,'"').replace(/&#39;/g,"'").trim();
    const allSel=shown.every(r=>s.sel.has(r.id));
    return `<style>@media(max-width:760px){.ltbl .keycol,.ltbl td.keycol,.ltbl th.keycol{display:none!important}}</style><table class="ltbl${regKey==='items'?' itbl':''}" style="${regKey==='items'?'width:100%':'width:max-content;min-width:100%'};table-layout:fixed"><colgroup><col style="width:44px">${cols.map(c=>`<col class="${c.cls||''}" style="width:${c.pct?c.pct+'%':c.auto?'auto':widthOf(c)+'px'}">`).join('')}<col style="width:${regKey==='items'?100:104}px"><col style="width:52px"></colgroup>
      <thead><tr><th class="ck"><input type="checkbox" data-a="sel-all" data-reg="${regKey}" ${allSel?'checked':''} aria-label="Select all"></th>${cols.map(c=>`<th class="${c.num?'num':''} ${c.cls||''}">${esc(c.label)}</th>`).join('')}<th>Status</th><th></th></tr></thead>
      <tbody>${shown.map((r,i)=>{const g=opts.groupBy&&opts.groupBy(r);const gh=g!=null&&(i===0||opts.groupBy(shown[i-1])!==g)?`<tr class="grow"><td colspan="${cols.length+3}">${esc(g)} <span class="muted">${shown.filter(x=>opts.groupBy(x)===g).length}</span></td></tr>`:'';return gh+`<tr class="clk ${r.status==='archived'?'arch':''}" data-a="row-open" data-reg="${regKey}" data-id="${r.id}">
        <td class="ck" data-label=""><input type="checkbox" data-a="sel-one" data-reg="${regKey}" data-id="${r.id}" ${s.sel.has(r.id)?'checked':''} aria-label="Select" title="Shift-click selects a range"></td>
        ${cols.map(c=>{const h=c.html(r);return `<td class="${c.num?'num':''} ${c.cls||''}" data-label="${esc(c.label)}" ${c.wrap?'style="white-space:normal;overflow:visible"':`title="${esc(plain(h))}"`}>${h}</td>`;}).join('')}<td data-label="Status"${regKey==='items'?' style="white-space:normal"':''}>${UI.statusTag(r.status)}${regKey==='items'&&REG.itemMissing(r).length?' '+UI.tag('Incomplete','warn'):''}</td>
        <td class="act" data-label=""><button class="btn icon gh" data-a="row-menu" data-reg="${regKey}" data-id="${r.id}" aria-label="Row actions">${ic('more')}</button></td></tr>`;}).join('')}</tbody></table>
      ${list.length>500?`<div class="empty">${shown.length} of ${list.length}</div>`:''}`;
  };

  /* Items: unit sits under the item name; departments wrap as tags */
  const daysTo=d=>{const t=Date.parse(d);return isNaN(t)?null:Math.ceil((t-Date.now())/864e5);};
  L.itemChips=function(r){
    const out=[];
    const st=REG.stockOf(r.id);const fd=d=>d?d.slice(8,10)+'/'+d.slice(5,7)+'/'+d.slice(0,4):'';
    if(REG.on(r.trackStock)){
      out.push(UI.tag(st.lots?st.qty+(st.expiresOn?' · exp '+fd(st.expiresOn).slice(0,5):' in stock'):'No stock yet',REG.lowStock(r)?'warn':'mut'));}
    if(REG.on(r.trackExpiry)&&st.expiresOn){const n=daysTo(st.expiresOn);if(n!=null&&n<=(+r.expiryAlertDays||0)){out.push(UI.tag(n<0?'Expired':'Expires in '+n+' d',n<0?'dng':'warn'));return out.join('');}}
    if(REG.on(r.trackCheck)&&+r.checkEvery)out.push(UI.tag('Check every '+r.checkEvery+' d','mut'));
    const uses=REG.itemUses(r.id);const sops=uses.filter(u=>u.type==='sop');
    if(sops.length===1)out.push(UI.tag(sops[0].label.replace(/^Individual /,'').replace(/^./,c=>c.toUpperCase())+' SOP','info'));
    else if(sops.length)out.push(UI.tag(sops.length+' SOPs','info'));
    if(+r.protocols)out.push(UI.tag(r.protocols+' protocol'+(+r.protocols===1?'':'s'),'mut'));
    if(r.plan)out.push(UI.tag('Vaccination plan','mut'));
    if(r.feedConfig)out.push(UI.tag('Feed Config','mut'));
    return out.slice(0,2).join('')+(out.length>2?UI.tag('+'+(out.length-2),'mut'):'');
  };
  /* Items: unit, subcategory path and departments sit under the item name; tracking chips */
  const ITEM_COLS=[
    {label:'Item',auto:1,wrap:1,html:r=>{const path=REG.catPath(r.categoryId).split(' > ');const p=(window.ItemsCat?path.slice(1):path).join(' › ');
      const sub=[r.unit,p,(r.depts||[]).map(REG.enumLabel).join(', ')].filter(Boolean).join(' · ');
      return `<b>${esc(r.name)}</b>${sub?`<div class="muted small">${esc(sub)}</div>`:''}`;}},
    {label:'Tracking',w:210,wrap:1,html:r=>{const c=L.itemChips(r);return c?`<div class="chips" style="display:flex;flex-wrap:wrap;gap:4px">${c}</div>`:'';}}];

  /* archive blocked: "In use by 3 animals" links to those records, selected for bulk actions */
  const ROUTE={tags:'shed-tags'};
  L.blockToast=function(coll,ids,title){
    if(coll==='items'){const u=[].concat(ids).map(id=>REG.itemUses(id,null,true).filter(x=>x.published)).flat();if(u.length){const f=u[0];UI.toast(title||'Cannot archive',null,{label:'Used in '+f.label+(u.length>1?' +'+(u.length-1):''),link:true,run:()=>{location.hash=f.href;}});return true;}}
    const agg=[];[].concat(ids).forEach(id=>REG.activeUsers(coll,id).forEach(u=>{const cur=agg.find(a=>a.regKey===u.regKey);if(cur)u.ids.forEach(i=>{if(!cur.ids.includes(i))cur.ids.push(i);});else agg.push({regKey:u.regKey,label:u.label,ids:u.ids.slice()});}));
    if(!agg.length)return false;
    const txt='In use by '+agg.map(a=>a.ids.length+' '+a.label.toLowerCase()).join(', ');
    const names=[].concat(ids).map(id=>REG.labelById(coll,id)).filter(Boolean);
    UI.toast(title||'Cannot archive',null,{label:txt,link:true,run:()=>{
      const u=agg[0],s=state(u.regKey);
      s.only={ids:u.ids,coll,label:'Using '+(names.length===1?names[0]:names.length+' '+REG.R[Object.keys(REG.R).find(k=>REG.R[k].coll===coll)].label.toLowerCase())};
      s.sel=new Set(u.ids);s.status='active';s.q='';s.f={};
      const h='#/configuration/items/'+(ROUTE[u.regKey]||u.regKey);
      if(location.hash===h)App.render();else location.hash=h;}});
    return true;
  };
  L.showIds=function(regKey,ids,label,coll){const s=state(regKey);s.only={ids,coll:coll||REG.R[regKey].coll,label:label||'Selected'};s.sel=new Set(ids);s.status='active';s.q='';s.f={};
    const h='#/configuration/items/'+(ROUTE[regKey]||regKey);App._allow=true;if(location.hash===h)App.render();else location.hash=h;};
  const archiveOne=(reg,rec)=>{if(REG.archiveBlock(reg.coll,rec.id))return L.blockToast(reg.coll,rec.id);
    const d=reg.coll==='items'?REG.itemUses(rec.id,null,true).filter(u=>u.type==='sop'&&!u.published):[];
    UI.undoable('Archived'+(d.length?' · used in draft of '+d[0].label+(d.length>1?' +'+(d.length-1):''):''),()=>{rec.status='archived';App.render();});};

  L.opts={}; // regKey -> opts used on current screen (set by page renderers)
  L.refreshTable=function(regKey){const el=document.querySelector(`[data-ltable="${regKey}"]`);if(el)el.innerHTML=L.table(regKey,L.opts[regKey]||{});};

  /* actions */
  Object.assign(A,{
    'lstatus'(el){state(el.dataset.reg).status=el.dataset.v;App.render();},
    'sel-all'(el,e){e.stopPropagation();const k=el.dataset.reg,s=state(k);const list=rows(k,L.opts[k]).slice(0,500);if(el.checked)list.forEach(r=>s.sel.add(r.id));else list.forEach(r=>s.sel.delete(r.id));App.render();},
    'sel-one'(el,e){e.stopPropagation();const k=el.dataset.reg,s=state(k),id=el.dataset.id;
      if(e.shiftKey&&s.anchor&&s.anchor!==id){
        const ids=rows(k,L.opts[k]).slice(0,500).map(r=>r.id);const a=ids.indexOf(s.anchor),b=ids.indexOf(id);
        if(a>=0&&b>=0){ids.slice(Math.min(a,b),Math.max(a,b)+1).forEach(x=>el.checked?s.sel.add(x):s.sel.delete(x));s.anchor=id;return App.render();}
      }
      el.checked?s.sel.add(id):s.sel.delete(id);s.anchor=id;App.render();},
    'bulk-clear'(el){state(el.dataset.reg).sel.clear();App.render();},
    'row-open'(el,e){if(e.target.closest('[data-a="row-menu"],input'))return;const k=el.dataset.reg;const o=L.opts[k]||{};if(o.onOpen)return o.onOpen(el.dataset.id);UI.recordForm(k,el.dataset.id);},
    'rec-new'(el){const k=el.dataset.reg;const o=L.opts[k]||{};if(o.onNew)return o.onNew();UI.recordForm(k,null,{defaults:o.defaults&&o.defaults()});},
    'grid-add'(el){const k=el.dataset.reg;const o=L.opts[k]||{};Sheet.openEntry(k,null,[o.defaults?o.defaults():{}]);},
    'row-menu'(el,e){
      e.stopPropagation(); const k=el.dataset.reg,id=el.dataset.id,reg=REG.R[k],rec=S.get(reg.coll,id),o=L.opts[k]||{};
      const low=((L.opts[k]||{}).columns||[]).filter(c=>c.low&&window.matchMedia('(max-width:1180px)').matches).map(c=>({label:c.label+': '+(String(c.html(rec)).replace(/<[^>]*>/g,'').replace(/&amp;/g,'&').trim()||'—'),disabled:1}));
      UI.menu(el,[...low,...(low.length?['-']:[]),
        {label:'Edit',icon:'edit-3',run:()=>o.onOpen?o.onOpen(id):UI.recordForm(k,id)},
        {label:'Edit in grid',icon:'sheet',run:()=>Sheet.openEntry(k,[id])},
        {label:'Duplicate',icon:'copy',run:()=>UI.recordForm(k,id,{duplicate:true})},
        rec.status==='archived'?{label:'Restore',icon:'rotate',run:()=>UI.undoable('Restored',()=>{rec.status='active';App.render();})}
          :{label:'Archive',icon:'archive',run:()=>archiveOne(reg,rec)},
        '-',
        {label:'Delete',icon:'trash',danger:true,run:()=>{const u=REG.usage(reg.coll,id);if(u.length){if(L.blockToast(reg.coll,id,'Cannot delete'))return;UI.toast('In use · '+u.map(x=>x.n+' '+x.label.toLowerCase()).join(', '),null,{label:'Archive',run:()=>archiveOne(reg,rec)});return;}
          UI.undoable('Deleted',()=>{S.remove(reg.coll,id);state(k).sel.delete(id);App.render();});}}
      ]);
    },
    'bulk-status'(el){const k=el.dataset.reg,s=state(k),reg=REG.R[k];let ids=[...s.sel];let kept=0;
      if(el.dataset.v==='archived'){const free=ids.filter(id=>!REG.archiveBlock(reg.coll,id));kept=ids.length-free.length;ids=free;if(!ids.length)return L.blockToast(reg.coll,[...s.sel]);}
      UI.undoable((el.dataset.v==='archived'?'Archived ':'Restored ')+ids.length+(kept?' · '+kept+' in use kept':''),()=>{ids.forEach(id=>{const r=S.get(reg.coll,id);if(r)r.status=el.dataset.v;});s.sel.clear();App.render();});},
    /* animals: move selection to a pen; partition is cleared unless one of THAT pen's partitions is chosen */
    'bulk-move'(el){const k=el.dataset.reg,s=state(k);const ids=[...s.sel];const f={park:'',pen:'',part:''};
      const body=()=>{const pens=S.active('pens').filter(p=>p.parkId===f.park),parts=S.active('partitions').filter(p=>p.penId===f.pen);
        const cap=f.pen&&S.get('pens',f.pen).capacity;const load=f.pen?S.active('animals').filter(a=>a.penId===f.pen&&!ids.includes(a.id)).length+ids.length:0;
        return `<div class="fgrid"><div class="fld full"><label>Park *</label><select data-mv="park"><option value="">Choose park</option>${S.active('parks').map(p=>`<option value="${p.id}" ${f.park===p.id?'selected':''}>${esc(p.name)}</option>`).join('')}</select></div>
          <div class="fld full"><label>Pen *</label><select data-mv="pen" ${f.park?'':'disabled'}><option value="">Choose pen</option>${pens.map(p=>`<option value="${p.id}" ${f.pen===p.id?'selected':''}>${esc(REG.label('pens',p))}</option>`).join('')}</select></div>
          <div class="fld full"><label>Partition</label><select data-mv="part" ${f.pen&&parts.length?'':'disabled'}><option value="">${parts.length?'No partition':'This pen has no partitions'}</option>${parts.map(p=>`<option value="${p.id}" ${f.part===p.id?'selected':''}>${esc(REG.fullLabel('partitions',p))}</option>`).join('')}</select>
</div>
          ${cap&&load>cap?`<div class="fld full"><div class="ferr w">Over capacity ${load}/${cap}</div></div>`:''}</div>`;};
      UI.drawer({title:'Move '+ids.length+' animal'+(ids.length===1?'':'s')+' to pen',body:body(),foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-mv-go>Move</button>`,
        mount(dr){dr.querySelectorAll('[data-mv]').forEach(x=>x.onchange=()=>{const key=x.dataset.mv;f[key]=x.value;if(key==='park'){f.pen='';f.part='';}if(key==='pen')f.part='';UI.redrawDrawer({body:body()});});
          dr.querySelector('[data-mv-go]').onclick=()=>{if(!f.park||!f.pen)return UI.toast('Choose a park and pen');
            const part=f.part&&S.get('partitions',f.part);if(part&&part.penId!==f.pen)return UI.toast('Partition is not in that pen');
            UI.closeDrawer();UI.undoable('Moved '+ids.length+' to '+REG.labelById('pens',f.pen)+(part?' - '+part.name:''),()=>{ids.forEach(id=>{const a=S.get('animals',id);if(a){a.parkId=f.park;a.penId=f.pen;a.partitionId=part?part.id:'';}});s.sel.clear();App.render();});};}});},
    'bulk-stage'(el){L.pickApply(el.dataset.reg,'stages','Set stage','Choose stage',(a,st)=>{if(a.speciesId!==st.speciesId)return false;a.stageId=st.id;return true;});},
    'bulk-set'(el){const coll=el.dataset.coll,attr={breeds:'breedId',sexes:'sexId'}[coll];const one=REG.R[coll].one;
      L.pickApply(el.dataset.reg,coll,'Set '+one.toLowerCase(),'Choose '+one.toLowerCase(),(a,x)=>{if(a.speciesId!==x.speciesId)return false;a[attr]=x.id;return true;});},
    'lonly-clear'(el){const s=state(el.dataset.reg);s.only=null;s.sel.clear();App.render();},
    'bulk-tag'(el){L.pickApply(el.dataset.reg,'tags','Add tag','Choose shed tag',(a,t)=>{if(t.speciesId&&t.speciesId!==a.speciesId)return false;a.tagIds=[...new Set((a.tagIds||[]).concat(t.id))];return true;});},
    'bulk-del'(el){const k=el.dataset.reg,s=state(k),reg=REG.R[k];const ids=[...s.sel];const used=ids.filter(id=>REG.usage(reg.coll,id).length);const free=ids.filter(id=>!used.includes(id));
      if(!free.length){if(L.blockToast(reg.coll,used,'Cannot delete'))return;UI.toast(used.length+' in use · archive instead');return;}
      UI.undoable('Deleted '+free.length+(used.length?' · '+used.length+' in use kept':''),()=>{S.remove(reg.coll,free);free.forEach(id=>s.sel.delete(id));App.render();});},
    'bulk-dup'(el){const k=el.dataset.reg,s=state(k),reg=REG.R[k];const ids=[...s.sel];
      Sheet.openEntry(k,null,ids.map(id=>UI.dupRow(k,S.get(reg.coll,id))));},
    'bulk-grid'(el){const k=el.dataset.reg;Sheet.openEntry(k,[...state(k).sel]);},
    'bulk-export'(el){const k=el.dataset.reg;IO.exportRegister(k,'csv',[...state(k).sel]);},
    'io-menu'(el){const k=el.dataset.reg;UI.menu(el,[
      {label:'Add in grid',icon:'sheet',run:()=>{const o=L.opts[k]||{};Sheet.openEntry(k,null,[o.defaults?o.defaults():{}]);}},
      {label:'Import file',icon:'upload',run:()=>IO.start(k)},
      '-',
      ...(k==='animals'?S.active('species').map(sp=>({label:sp.name+' template (.xlsx)',icon:'download',run:()=>IO.template(k,'xlsx',sp.id)})):[{label:'Template (.xlsx)',icon:'download',run:()=>IO.template(k,'xlsx')}]),
      {label:'Template (.csv)',icon:'download',run:()=>IO.template(k,'csv')},
      '-',
      {label:'Export (.xlsx)',icon:'download',run:()=>IO.exportRegister(k,'xlsx')},
      {label:'Export (.csv)',icon:'download',run:()=>IO.exportRegister(k,'csv')},
      ...(KEYS_HIDDEN.includes(k)?['-',{label:state(k).showKeys?'Hide keys':'Show keys',icon:'eye',run:()=>{const s=state(k);s.showKeys=!s.showKeys;App.render();}}]:[])]);}
  });

  /* bulk "pick one existing value and apply" (stage / tag), species-checked per animal */
  L.pickApply=function(k,coll,title,ph,apply){
    const s=state(k);const ids=[...s.sel];const sp=new Set(ids.map(id=>(S.get('animals',id)||{}).speciesId));
    const opts=S.active(coll).filter(x=>coll==='tags'?x.kind!=='group'&&(!x.speciesId||sp.has(x.speciesId)):sp.has(x.speciesId));
    let pick='';
    UI.drawer({title:title+' · '+ids.length+' animal'+(ids.length===1?'':'s'),
      body:`<div class="fgrid"><div class="fld full"><label>${esc(REG.R[coll].one)} *</label><select data-pa><option value="">${esc(ph)}</option>${opts.map(x=>`<option value="${x.id}">${esc(REG.fullLabel(coll,x)+(coll==='tags'&&!x.speciesId?' · All species':''))}</option>`).join('')}</select></div></div>`,
      foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-pa-go>Apply</button>`,
      mount(dr){dr.querySelector('[data-pa]').onchange=e=>pick=e.target.value;
        dr.querySelector('[data-pa-go]').onclick=()=>{const rec=S.get(coll,pick);if(!rec)return UI.toast(ph);
          const ok=ids.filter(id=>{const a=S.get('animals',id);return a&&(coll==='tags'?(!rec.speciesId||rec.speciesId===a.speciesId):rec.speciesId===a.speciesId);});
          const skipped=ids.length-ok.length;UI.closeDrawer();
          if(!ok.length)return UI.toast('No selected animal is '+REG.labelById('species',rec.speciesId));
          UI.undoable(title.replace(/^Set /,'Set ').replace(/^Add /,'Added ')+' · '+ok.length+(skipped?' · '+skipped+' other species skipped':''),()=>{ok.forEach(id=>apply(S.get('animals',id),rec));s.sel.clear();App.render();});};}});
  };

  document.addEventListener('input',e=>{
    const s=e.target.closest('[data-lsearch]'); if(s){state(s.dataset.lsearch).q=s.value;L.refreshTable(s.dataset.lsearch);}
  });
  document.addEventListener('change',e=>{
    const f=e.target.closest('[data-lfilter]'); if(f){state(f.dataset.lfilter).f[f.dataset.col]=f.value;App.render();}
  });
  window.List=L;
})();
