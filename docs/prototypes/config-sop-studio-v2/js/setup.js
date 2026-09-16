/* Configuration / Items and settings: register rail + register panel, park page, animal types editor */
(function(){
  const P={};
  const SECTIONS=[
    ['Farm places',[['parks','Parks'],['pens','Pens'],['partitions','Partitions'],['farms','Farms']]],
    ['Animal types',[['species','Species'],['breeds','Breeds'],['sexes','Sexes'],['stages','Lifecycle stages'],['groups','Tags & groups'],['healthStates','Health states']]],
    ['Animals',[['animals','Animals']]],
    ['Catalogue',[['items','Items & categories']]],
    ['People & approvers',[['people','People'],['roles','Roles'],['approvers','Approvers']]],
    ['Vendors & trucks',[['vendors','Vendors'],['trucks','Trucks']]],
    ['Reference lists',[['statusDefs','Status definitions'],['exitReasons','Exit reasons'],['purposes','Animal purposes'],['movementReasons','Movement reasons'],['weightBands','Weight bands'],['rationGroups','Ration groups'],['diseases','Diseases'],['symptoms','Symptoms'],['deathCauses','Death causes'],['marketCities','Market cities'],['sopCategories','SOP categories'],['taskTypes','Task types']]],
    ['Business settings',[['settings','Business settings']]]
  ];
  const regOf=k=>k==='shed-tags'||k==='groups'?'tags':k;
  const count=k=>{if(k==='groups')return S.active('tags').length;return S.active(REG.R[regOf(k)].coll).length;};
  const penDisplay=a=>{const pen=S.get('pens',a.penId),pt=S.get('partitions',a.partitionId);return pen?pen.name+(pt?' - '+pt.name:''):'';};
  const num=v=>`<span class="num">${esc(v)}</span>`;

  function header(){
    return `<div class="phead"><div><div class="crumb">Configuration / <b>Items and settings</b></div><h1>Items and settings</h1></div><div class="sp"></div>
      <button class="btn out" data-a="wb-menu">${ic('sheet')}Farm setup workbook</button></div>`;
  }
  function rail(active){
    const sec=SECTIONS.find(s=>s[1].some(x=>x[0]===active));
    return `<nav class="cfgrail card" aria-label="Registers">${SECTIONS.map(([t,items])=>`<div class="rsec"><div class="eyebrow">${esc(t)}</div>${items.map(([k,l])=>
        `<a class="ritem ${k===active?'on':''}" href="#/configuration/items/${k}"><span>${esc(l)}</span><span class="rct">${count(k)}</span></a>`).join('')}</div>`).join('')}</nav>
      <select class="fsel cfgrail-m" data-railsel aria-label="Register">${SECTIONS.map(([t,items])=>`<optgroup label="${esc(t)}">${items.map(([k,l])=>`<option value="${k}" ${k===active?'selected':''}>${esc(l)} (${count(k)})</option>`).join('')}</optgroup>`).join('')}</select>`;
  }
  document.addEventListener('change',e=>{if(e.target.matches&&e.target.matches('[data-railsel]'))location.hash='#/configuration/items/'+e.target.value;});

  function list(regKey,opts){opts=opts||{};List.opts[regKey]=opts;return List.html(regKey,opts);}

  const PANELS={
    parks:()=>list('parks',{eyebrow:'Farm places',filters:['farm'],onOpen:id=>location.hash='#/configuration/items/park/'+id,onNew:()=>location.hash='#/configuration/items/park/new',addLabel:'Add park',
      columns:[{label:'Park',html:r=>`<b>${esc(r.name)}</b>`},{label:'Code',w:150,html:r=>`<span class="mono">${esc(r.code||'')}</span>`},{label:'Farm',html:r=>esc(REG.labelById('farms',r.farmId))},
        {label:'Pens',w:80,num:1,html:r=>S.active('pens').filter(p=>p.parkId===r.id).length},{label:'Capacity',w:100,num:1,html:r=>{const t=S.active('pens').filter(p=>p.parkId===r.id).reduce((a,p)=>a+(+p.capacity||0),0);return t||'—';}},
        {label:'Animals',w:90,num:1,html:r=>S.active('animals').filter(a=>a.parkId===r.id).length}]}),
    pens:()=>list('pens',{eyebrow:'Farm places',filters:['park','stage','sex'],
      columns:[{label:'Pen',html:r=>`<b>${esc(r.name)}</b>`},{label:'Park',html:r=>esc(REG.labelById('parks',r.parkId))},{label:'Partitions',w:100,num:1,html:r=>S.active('partitions').filter(p=>p.penId===r.id).length},
        {label:'Animals',w:90,num:1,html:r=>S.active('animals').filter(a=>a.penId===r.id).length},{label:'Capacity',w:90,num:1,html:r=>{const n=S.active('animals').filter(a=>a.penId===r.id).length;return (r.capacity===''||r.capacity==null?'—':esc(r.capacity))+(r.capacity&&n>r.capacity?' '+UI.tag('Over','dng'):'');}},
        {label:'Stage',html:r=>esc(REG.labelById('stages',r.stageId))},{label:'Sex',w:90,html:r=>esc(r.sex||'')}]}),
    partitions:()=>list('partitions',{eyebrow:'Farm places',filters:['park','pen'],
      columns:[{label:'Partition',html:r=>`<b>${esc(REG.fullLabel('partitions',r))}</b>`},{label:'Park',html:r=>{const p=S.get('pens',r.penId);return esc(p?REG.labelById('parks',p.parkId):'');}},
        {label:'Capacity',w:100,num:1,html:r=>esc(r.capacity===''||r.capacity==null?'—':r.capacity)},{label:'Animals',w:90,num:1,html:r=>S.active('animals').filter(a=>a.partitionId===r.id).length}]}),
    farms:()=>list('farms',{eyebrow:'Farm places',columns:[{label:'Code',w:90,html:r=>`<b class="mono">${esc(r.code)}</b>`},{label:'Farm',html:r=>esc(r.name)},{label:'Type',w:110,html:r=>esc(r.kind||'')},{label:'Parks',w:80,num:1,html:r=>S.active('parks').filter(p=>p.farmId===r.id).length}]}),
    species:()=>list('species',{eyebrow:'Animal types',onOpen:id=>location.hash='#/configuration/items/animal-types/'+id,onNew:()=>location.hash='#/configuration/items/animal-types/new',addLabel:'New animal types',
      extraBtns:`<a class="btn sm" href="#/configuration/items/animal-types/all">${ic('edit-3')}Edit together</a>`,
      columns:[{label:'Species',html:r=>`<b>${esc(r.name)}</b>`},{label:'Code',w:90,html:r=>`<span class="mono">${esc(r.code||'')}</span>`},
        ...['breeds','sexes','stages'].map(c=>({label:REG.R[c].label.replace('Lifecycle ',''),w:90,num:1,html:r=>S.active(c).filter(x=>x.speciesId===r.id).length})),
        {label:'Animals',w:90,num:1,html:r=>S.active('animals').filter(a=>a.speciesId===r.id).length}]}),
    breeds:()=>list('breeds',{eyebrow:'Animal types',filters:['species']}),
    sexes:()=>list('sexes',{eyebrow:'Animal types',filters:['species']}),
    stages:()=>list('stages',{eyebrow:'Animal types',filters:['species','sex'],columns:[
      {label:'Stage',html:r=>`<b>${esc(r.name)}</b>`},{label:'Code',w:100,html:r=>`<span class="mono">${esc(r.code)}</span>`},{label:'Species',w:110,html:r=>esc(REG.labelById('species',r.speciesId))},
      {label:'From (days)',w:110,num:1,html:r=>esc(r.fromD===''||r.fromD==null?'—':r.fromD)},{label:'To (days)',w:100,num:1,html:r=>esc(r.toD===''||r.toD==null?'—':r.toD)},{label:'Sex',w:90,html:r=>esc(r.sex)}]}),
    'shed-tags':()=>list('tags',{eyebrow:'Animal types',title:'Shed tags',filters:['species'],where:r=>r.kind!=='group',defaults:()=>({kind:'tag'}),addLabel:'Add shed tag'}),
    groups:()=>list('tags',{eyebrow:'Animal types',title:'Tags & groups',filters:['species','kind'],addLabel:'Add tag or group'}),
    healthStates:()=>list('healthStates',{eyebrow:'Animal types',filters:['group']}),
    animals:()=>list('animals',{eyebrow:'Animals',filters:['species','park','pen','stage','sex'],addLabel:'Add animals',onNew:()=>Sheet.openEntry('animals',null,[{}]),
      extraBtns:`<button class="btn sm" data-a="rec-one" data-reg="animals">${ic('plus')}Add one</button>`,
      columns:[{label:'RFID',html:r=>`<b class="mono">${esc(r.rfid)}</b>`},{label:'Second tag',html:r=>`<span class="muted">${esc(r.tag2||'')}</span>`},
        {label:'Species',html:r=>esc(REG.labelById('species',r.speciesId))},{label:'Breed',html:r=>esc(REG.labelById('breeds',r.breedId))},
        {label:'Sex',w:80,html:r=>esc(REG.labelById('sexes',r.sexId))},{label:'Stage',html:r=>esc(REG.labelById('stages',r.stageId))},
        {label:'Park',html:r=>esc(REG.labelById('parks',r.parkId))},{label:'Pen',html:r=>esc(penDisplay(r))},{label:'Weight (kg)',w:100,num:1,html:r=>esc(r.weight===''||r.weight==null?'':r.weight)}]}),
    items:()=>items(),
    people:()=>list('people',{eyebrow:'People & approvers',filters:['role','parks'],columns:[{label:'Name',html:r=>`<b>${esc(r.name)}</b>`},{label:'Role',html:r=>esc(REG.labelById('roles',r.roleId))},
      {label:'Parks',html:r=>(r.parkIds||[]).length?(r.parkIds||[]).map(id=>UI.tag(REG.labelById('parks',id),'mut')).join(' '):'<span class="muted">All parks</span>'},{label:'Email',html:r=>esc(r.email||'')},{label:'Phone',html:r=>esc(r.phone||'')}]}),
    roles:()=>list('roles',{eyebrow:'People & approvers',columns:[{label:'Role',html:r=>`<b>${esc(r.name)}</b>`},{label:'Grade',w:140,html:r=>esc(r.grade||'')},{label:'People',w:90,num:1,html:r=>S.active('people').filter(p=>p.roleId===r.id).length},{label:'Approves',w:100,num:1,html:r=>S.active('approvers').filter(p=>p.roleId===r.id).length}]}),
    approvers:()=>list('approvers',{eyebrow:'People & approvers',filters:['dept','role'],columns:[{label:'Department',w:160,html:r=>UI.tag(r.dept,'teal')},{label:'Approval step',html:r=>`<b>${esc(r.step)}</b>`},{label:'Approver role',html:r=>esc(REG.labelById('roles',r.roleId))}]}),
    vendors:()=>list('vendors',{eyebrow:'Vendors & trucks',filters:['supplies','farm'],columns:[{label:'Vendor',html:r=>`<b>${esc(r.name)}</b>`},{label:'Supplies',w:120,html:r=>UI.tag(r.supplies||'','mut')},{label:'Farm',html:r=>esc(REG.labelById('farms',r.farmId))},{label:'City',html:r=>esc(r.city||'')},{label:'Trucks',w:80,num:1,html:r=>S.active('trucks').filter(t=>t.vendorId===r.id).length}]}),
    trucks:()=>list('trucks',{eyebrow:'Vendors & trucks',filters:['vendor'],columns:[{label:'Vehicle number',html:r=>`<b class="mono">${esc(r.number)}</b>`},{label:'Vendor',html:r=>esc(REG.labelById('vendors',r.vendorId))},{label:'Capacity',w:110,num:1,html:r=>esc(r.capacity===''||r.capacity==null?'':r.capacity)}]}),
    settings:()=>list('settings',{eyebrow:'Business settings',filters:['dept'],
      columns:[{label:'Setting',html:r=>`<b>${esc(r.name)}</b>`},{label:'Department',w:160,html:r=>UI.tag(r.dept,'teal')},
        {label:'Value',w:200,html:r=>`<div class="valcell"><input class="inl num" data-setval="${r.id}" value="${esc(r.value)}" aria-label="${esc(r.name)}" ${r.type==='time'?'':'inputmode="decimal"'}><span class="u">${esc(r.unit||'')}</span></div>`}]})
      +`<section class="card mt"><div class="hd"><h3>Module configuration</h3></div>${[['Vaccination plan','#/vaccination/plan','Preventive Care'],['Feed Config','#/feed/config','Feed'],['Health Config','#/health/config','Health'],['Sales Config','#/sales/config','Sales']]
        .map(([l,h,dp])=>`<a class="linkrow" href="${h}"><b>${l}</b><span class="tag t-teal">${dp}</span><span class="sp"></span>${ic('chevron-right')}</a>`).join('')}</section>`
  };
  ['statusDefs','exitReasons','purposes','movementReasons','weightBands','rationGroups','diseases','symptoms','deathCauses','marketCities','sopCategories','taskTypes']
    .forEach(k=>PANELS[k]=()=>list(k,{eyebrow:'Reference lists',filters:REG.R[k].cols.filter(c=>c.type==='enum'||c.type==='ref').map(c=>c.k).slice(0,2)}));

  document.addEventListener('change',e=>{
    const t=e.target; if(!(t.matches&&t.matches('[data-setval]')))return;
    const s=S.get('settings',t.dataset.setval); if(!s)return;
    const raw=t.value.trim();
    if(s.type!=='time'&&(raw===''||isNaN(Number(raw)))){UI.toast(s.name+': number');t.value=s.value;return;}
    if(s.type==='time'&&!/^\d{1,2}:\d{2}$/.test(raw)){UI.toast(s.name+': HH:MM');t.value=s.value;return;}
    if(String(s.value)===raw)return;
    const n=S.snap('Setting'); s.value=s.type==='time'?raw:Number(raw); S.save();
    UI.toast(s.name+' · '+raw+' '+(s.unit||''),()=>{S.undoTo(n);App.render();});
  });
  A['rec-one']=el=>UI.recordForm(el.dataset.reg,null);

  /* ---------- items & categories ---------- */
  let catSel='',catQ='',catArch=false;
  function subtree(id){const out=new Set([id]);let grew=true;while(grew){grew=false;S.all('categories').forEach(c=>{if(out.has(c.parentId)&&!out.has(c.id)){out.add(c.id);grew=true;}});}return out;}
  function items(){
    const cats=S.all('categories').filter(c=>catArch?c.status==='archived':c.status!=='archived');
    const q=REG.norm(catQ);
    const show=c=>!q||[...subtree(c.id)].some(id=>{const x=S.get('categories',id);return x&&REG.norm(x.name).includes(q);});
    const node=(c,depth)=>{if(!show(c))return '';const kids=cats.filter(x=>(x.parentId||'')===c.id);const n=S.active('items').filter(i=>subtree(c.id).has(i.categoryId)).length;
      return `<div class="tn ${catSel===c.id?'on':''}" style="padding-left:${8+depth*16}px" data-a="cat-sel" data-id="${c.id}"><span class="tw">${ic(kids.length?'folder':'circle','',13)}</span><span class="tnl">${esc(c.name)}</span><span class="ct">${n}</span>
        <button class="btn icon" data-a="cat-menu" data-id="${c.id}" aria-label="Category actions">${ic('more','',15)}</button></div>${kids.map(k=>node(k,depth+1)).join('')}`;};
    const roots=catArch?cats.filter(c=>!c.parentId||!cats.some(x=>x.id===c.parentId)):cats.filter(c=>!c.parentId);
    const where=catSel?(r=>subtree(catSel).has(r.categoryId)):null;
    List.opts.items={eyebrow:'Catalogue',where,filters:['depts'],defaults:()=>catSel?{category:REG.catPath(catSel)}:{},
      columns:[{label:'Item',html:r=>`<b>${esc(r.name)}</b>`},{label:'Category',html:r=>`<span class="muted">${esc(REG.catPath(r.categoryId))}</span>`},{label:'Unit',w:80,html:r=>esc(r.unit||'')},
        {label:'Departments',html:r=>(r.depts||[]).map(d=>UI.tag(d,'teal')).join(' ')}]};
    return `<div class="split"><section class="card"><div class="hd"><h3>Categories</h3><span class="cnt">${S.active('categories').length}</span><span class="sp"></span>
        <button class="btn sm" data-a="io-menu" data-reg="categories">${ic('sheet')}Import / export</button></div>
      <div class="lbar"><label class="search">${ic('search')}<input data-catq value="${esc(catQ)}" placeholder="Search"></label>
        <div class="seg"><button class="${catArch?'':'on'}" data-a="cat-arch" data-v="">Active</button><button class="${catArch?'on':''}" data-a="cat-arch" data-v="1">Archived</button></div></div>
      ${catArch?'':`<div class="lbar"><input class="inl" style="flex:1" data-newcat placeholder="${catSel?esc(S.get('categories',catSel).name)+' › new subcategory':'New category'}"><button class="btn sm out" data-a="cat-add">${ic('plus')}Add</button></div>`}
      <div class="tree">${catArch?'':`<div class="tn ${catSel?'':'on'}" data-a="cat-sel" data-id=""><span class="tw">${ic('layers','',13)}</span><span class="tnl">All items</span><span class="ct">${S.active('items').length}</span></div>`}
      ${roots.map(c=>node(c,0)).join('')||'<div class="empty">None</div>'}</div></section>
      <div>${List.html('items',List.opts.items)}</div></div>`;
  }
  document.addEventListener('input',e=>{if(e.target.matches&&e.target.matches('[data-catq]')){catQ=e.target.value;const p=e.target.selectionStart;App.render();const n=document.querySelector('[data-catq]');if(n){n.focus();n.setSelectionRange(p,p);}}});
  Object.assign(A,{
    'cat-arch'(el){catArch=!!el.dataset.v;catSel='';App.render();},
    'cat-sel'(el,e){if(e.target.closest('[data-a="cat-menu"]'))return;catSel=el.dataset.id;App.render();},
    'cat-add'(){const i=document.querySelector('[data-newcat]');const v=i&&i.value.trim();if(!v)return;
      if(S.active('categories').some(c=>(c.parentId||'')===catSel&&REG.norm(c.name)===REG.norm(v))){UI.toast('“'+v+'” already exists');return;}
      const n=S.snap('Added category');S.add('categories',{name:v,parentId:catSel});S.save();App.render();UI.toast('Added '+v,()=>{S.undoTo(n);App.render();});},
    'cat-menu'(el,e){e.stopPropagation();const c=S.get('categories',el.dataset.id);UI.menu(el,[
      {label:'Add subcategory',icon:'plus',run(){catSel=c.id;catArch=false;App.render();setTimeout(()=>{const i=document.querySelector('[data-newcat]');if(i)i.focus();},0);}},
      {label:'Rename / move',icon:'edit-3',run(){UI.drawer({title:'Category',body:`<div class="fld"><label>Name</label><input data-cren value="${esc(c.name)}"></div><div class="fld"><label>Parent</label><select data-cpar><option value="">Top level</option>${S.active('categories').filter(x=>!subtree(c.id).has(x.id)).map(x=>`<option value="${x.id}" ${x.id===c.parentId?'selected':''}>${esc(REG.catPath(x.id))}</option>`).join('')}</select></div>`,
        foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-crsave>Save</button>`,
        mount(dr){dr.querySelector('[data-crsave]').onclick=()=>{const nm=dr.querySelector('[data-cren]').value.trim(),par=dr.querySelector('[data-cpar]').value;if(!nm)return;
          if(S.active('categories').some(x=>x.id!==c.id&&(x.parentId||'')===par&&REG.norm(x.name)===REG.norm(nm))){UI.toast('“'+nm+'” already exists there');return;}
          UI.closeDrawer();UI.undoable('Saved category',()=>{c.name=nm;c.parentId=par;App.render();});};}});}},
      c.status==='archived'?{label:'Restore',icon:'rotate',run(){UI.undoable('Restored',()=>{subtree(c.id).forEach(id=>{const x=S.get('categories',id);if(x)x.status='active';});let p=S.get('categories',c.parentId);while(p){p.status='active';p=S.get('categories',p.parentId);}App.render();});}}
        :{label:'Archive',icon:'archive',run(){UI.undoable('Archived category',()=>{subtree(c.id).forEach(id=>{const x=S.get('categories',id);if(x)x.status='archived';});if(subtree(c.id).has(catSel))catSel='';App.render();});}},
      '-',{label:'Delete',icon:'trash',danger:true,run(){const ids=subtree(c.id);const n=S.all('items').filter(i=>ids.has(i.categoryId)).length;
        if(n){UI.toast('In use by '+n+' items · archive instead');return;}
        UI.undoable('Deleted category',()=>{S.remove('categories',[...ids]);catSel='';App.render();});}}]);}
  });
  document.addEventListener('keydown',e=>{if(e.target.matches&&e.target.matches('[data-newcat]')&&e.key==='Enter')A['cat-add']();});

  /* ---------- park page with pens grid ---------- */
  let PD=null;
  function parkDraft(id){
    if(PD&&PD.id===id)return PD;
    const p=id!=='new'?S.get('parks',id):null;
    PD={id,snap:JSON.stringify(S.state),created:false,dirty:false,name:p?p.name:'',code:p?p.code:'',farm:p?REG.labelById('farms',p.farmId):'',
      pens:p?S.all('pens').filter(x=>x.parkId===p.id&&x.status!=='archived').map(x=>({id:x.id,name:x.name,capacity:x.capacity==null?'':x.capacity,parts:S.active('partitions').filter(t=>t.penId===x.id).length,stage:REG.labelById('stages',x.stageId),stageId:x.stageId||'',sex:x.sex||'mixed'})):[],
      gen:{n:4,cap:40,prefix:'Pen',start:1,parts:0},removed:[],err:{}};
    return PD;
  }
  function parkPage(id){
    const d=parkDraft(id); const e=d.err;
    const errTxt=k=>e[k]?`<div class="ferr">${esc(e[k])}</div>`:'';
    return `<div class="phead"><div><div class="crumb">Configuration / <a href="#/configuration/items/parks">Items and settings</a> / <b>${id==='new'?'New park':esc(d.name)}</b></div><h1>${id==='new'?'New park':esc(d.name)}</h1></div><div class="sp"></div>
      <button class="btn" data-a="pd-cancel">Cancel</button><button class="btn p" data-a="park-save">${ic('check')}Save park</button></div>
      <section class="card mb"><div class="bd"><div class="fgrid fg3">
        <div class="fld"><label>Farm</label><input data-pd="farm" value="${esc(d.farm)}" placeholder="Search or create" autocomplete="off">${errTxt('farm')}</div>
        <div class="fld"><label>Park name *</label><input data-pd="name" value="${esc(d.name)}">${errTxt('name')}</div>
        <div class="fld"><label>Code</label><input data-pd="code" value="${esc(d.code)}"></div>
      </div></div></section>
      <section class="card"><div class="hd"><h3>Pens <span class="cnt">${d.pens.length}</span></h3><span class="sp"></span>
        <button class="btn sm" data-a="pd-addpen">${ic('plus')}Pen</button></div>
        <div class="sheetbar genbar">
          <label class="gl">Number of pens<input class="inl num" data-pg="n" value="${esc(d.gen.n)}" inputmode="numeric"></label>
          <label class="gl">Capacity per pen<input class="inl num" data-pg="cap" value="${esc(d.gen.cap)}" inputmode="numeric"></label>
          <label class="gl">Name prefix<input class="inl" style="width:100px" data-pg="prefix" value="${esc(d.gen.prefix)}"></label>
          <label class="gl">Start number<input class="inl num" data-pg="start" value="${esc(d.gen.start)}" inputmode="numeric"></label>
          <label class="gl">Partitions per pen<input class="inl num" data-pg="parts" value="${esc(d.gen.parts)}" inputmode="numeric"></label>
          <button class="btn sm out" data-a="pd-gen">${ic('layers')}Generate</button>
        </div>
        <div class="twrap"><table class="mini" style="min-width:680px"><thead><tr><th>Pen *</th><th style="width:100px">Capacity</th><th style="width:100px">Partitions</th><th>Stage</th><th style="width:110px">Sex</th><th style="width:80px">Animals</th><th style="width:40px"></th></tr></thead><tbody>
        ${d.pens.length?d.pens.map((p,i)=>`<tr>
          <td><input data-pp="${i}" data-k="name" value="${esc(p.name)}" aria-label="Pen name" class="${e['pen'+i]?'bad':''}">${e['pen'+i]?`<div class="ferr">${esc(e['pen'+i])}</div>`:''}</td>
          <td><input data-pp="${i}" data-k="capacity" value="${esc(p.capacity)}" inputmode="numeric" aria-label="Capacity" class="${e['cap'+i]?'bad':''}"></td>
          <td><input data-pp="${i}" data-k="parts" value="${esc(p.parts)}" inputmode="numeric" aria-label="Partitions"></td>
          <td><input data-pp="${i}" data-k="stage" value="${esc(p.stage)}" placeholder="Search or create" autocomplete="off" aria-label="Stage" class="${e['stg'+i]?'bad':''}">${e['stg'+i]?`<div class="ferr">${esc(e['stg'+i])}</div>`:''}</td>
          <td><select data-pp="${i}" data-k="sex" aria-label="Sex">${['mixed','female','male'].map(s=>`<option ${p.sex===s?'selected':''}>${s}</option>`).join('')}</select></td>
          <td class="num muted">${p.id?S.active('animals').filter(a=>a.penId===p.id).length:'—'}</td>
          <td class="x"><button class="btn icon gh" data-a="pd-delpen" data-i="${i}" aria-label="Remove pen">${ic('x')}</button></td></tr>`).join('')
          :`<tr><td colspan="7" class="empty">No pens</td></tr>`}
        </tbody></table></div></section>`;
  }
  function stageCreates(text,after){
    return S.active('species').map(sp=>({label:'Create “'+text+'” · '+sp.name,run:()=>{const id=S.add('stages',{speciesId:sp.id,code:text.replace(/\s+/g,'').toUpperCase().slice(0,10),name:text,fromD:'',toD:'',sex:'any'}).id;PD.created=true;if(after)after(id);return id;}}));
  }
  function parkMount(root){
    const d=PD; if(!d)return;
    root.querySelectorAll('[data-pd]').forEach(inp=>{
      inp.oninput=()=>{d[inp.dataset.pd]=inp.value;d.dirty=true;if(inp.dataset.pd==='farm')pop();};
      if(inp.dataset.pd==='farm'){inp.onfocus=pop;inp.onkeydown=e=>UI.popKey(e);inp.onblur=()=>setTimeout(()=>{if(UI._popCfg&&UI._popCfg.input===inp)UI.closePop();},150);}
      function pop(){UI.pop(inp,{options:()=>S.active('farms').map(f=>({id:f.id,label:f.name,sub:f.code})),onPick(o){d.farm=inp.value;},
        creates:t=>[{label:'Create farm “'+t+'”',run:()=>{d.created=true;return REG.createRef('parks',REG.R.parks.cols.find(c=>c.k==='farm'),t,{});}}]});}
    });
    root.querySelectorAll('[data-pg]').forEach(inp=>inp.oninput=()=>{d.gen[inp.dataset.pg]=inp.value;});
    root.querySelectorAll('[data-pp]').forEach(inp=>{
      const p=d.pens[+inp.dataset.pp],k=inp.dataset.k;
      inp.oninput=inp.onchange=()=>{p[k]=inp.value;d.dirty=true;if(k==='stage'){p.stageId='';pop();}};
      if(k==='stage'){inp.onfocus=pop;inp.onkeydown=e=>UI.popKey(e);inp.onblur=()=>setTimeout(()=>{if(UI._popCfg&&UI._popCfg.input===inp)UI.closePop();},150);}
      function pop(){UI.pop(inp,{options:()=>S.active('stages').map(s=>({id:s.id,label:s.name,sub:s.code+' · '+REG.labelById('species',s.speciesId)})),
        onPick(o){p.stage=inp.value;p.stageId=o.id||'';},creates:t=>stageCreates(t,id=>{p.stageId=id;})});}
    });
  }
  Object.assign(A,{
    'pd-addpen'(){PD.pens.push({name:'',capacity:'',parts:0,stage:'',stageId:'',sex:'mixed'});PD.dirty=true;App.render();},
    'pd-delpen'(el){const p=PD.pens.splice(+el.dataset.i,1)[0];if(p&&p.id)PD.removed.push(p.id);PD.dirty=true;App.render();},
    'pd-gen'(){const g=PD.gen;const n=Math.min(200,Math.max(0,parseInt(g.n)||0));const st=parseInt(g.start)||1;const cap=parseInt(g.cap);
      if(isNaN(cap)||cap<0){UI.toast('Capacity: 0 or more');return;}
      const names=new Set(PD.pens.map(p=>REG.norm(p.name)));let made=0,i=st;
      while(made<n&&i<st+1000){const nm=(g.prefix||'Pen')+' '+i;i++;if(names.has(REG.norm(nm)))continue;PD.pens.push({name:nm,capacity:cap,parts:Math.max(0,parseInt(g.parts)||0),stage:'',stageId:'',sex:'mixed'});made++;}
      PD.dirty=true;App.render();UI.toast(made+' pens added');},
    'pd-cancel'(){App.leave('#/configuration/items/parks');},
    'park-save'(){
      const d=PD; d.err={};
      if(!d.name.trim())d.err.name='Required';
      const farmCol=REG.R.parks.cols.find(c=>c.k==='farm');
      const farm=d.farm.trim()?REG.findRef('parks',farmCol,d.farm,{}):{rec:null};
      if(d.farm.trim()&&!farm.rec)d.err.farm='Not found';
      const farmId=farm.rec?farm.rec.id:'';
      if(d.name.trim()&&S.all('parks').some(x=>x.id!==d.id&&(x.farmId||'')===farmId&&REG.norm(x.name)===REG.norm(d.name)))d.err.name='Already exists';
      const seen={};
      d.pens.forEach((p,i)=>{const nm=REG.norm(p.name);if(!nm)d.err['pen'+i]='Required';else if(seen[nm]!=null)d.err['pen'+i]='Duplicate';seen[nm]=i;
        if(p.capacity!==''&&(isNaN(Number(p.capacity))||Number(p.capacity)<0))d.err['cap'+i]='0 or more';
        if(String(p.stage).trim()&&!p.stageId){const hits=S.active('stages').filter(x=>REG.norm(x.name)===REG.norm(p.stage)||REG.norm(x.code)===REG.norm(p.stage));if(hits.length===1)p.stageId=hits[0].id;else d.err['stg'+i]=hits.length?'Pick species variant':'Not found';}});
      if(Object.keys(d.err).length){App.render();UI.toast('Fix highlighted fields');return;}
      const n=S.snap('Saved park');
      let park=d.id!=='new'?S.get('parks',d.id):null;
      if(park)Object.assign(park,{name:d.name.trim(),code:d.code.trim(),farmId});
      else park=S.add('parks',{name:d.name.trim(),code:d.code.trim(),farmId});
      d.removed.forEach(id=>{const p=S.get('pens',id);if(p)p.status='archived';});
      d.pens.forEach(p=>{
        let pen=p.id?S.get('pens',p.id):null;
        const attrs={parkId:park.id,name:String(p.name).trim(),capacity:p.capacity===''?'':Number(p.capacity),stageId:String(p.stage).trim()?p.stageId:'',sex:p.sex};
        if(pen)Object.assign(pen,attrs); else pen=S.add('pens',attrs);
        const want=Math.max(0,parseInt(p.parts)||0);
        const have=S.active('partitions').filter(t=>t.penId===pen.id);
        for(let k=have.length+1;k<=want;k++)S.add('partitions',{penId:pen.id,name:'Part '+k,capacity:''});
        have.slice(want).forEach(t=>t.status='archived');
      });
      S.save(); PD=null;
      App.leave('#/configuration/items/parks',true);
      UI.toast('Saved '+park.name,()=>{S.undoTo(n);App.render();});
    }
  });

  /* ---------- animal types editor (many species together) ---------- */
  let ED=null;
  function blockFrom(s){
    const of=c=>S.active(c).filter(x=>x.speciesId===s.id);
    return {id:s.id,name:s.name,code:s.code||'',breeds:of('breeds').map(b=>({id:b.id,name:b.name})),sexes:of('sexes').map(b=>({id:b.id,name:b.name})),
      stages:of('stages').map(t=>({id:t.id,code:t.code,name:t.name,fromD:t.fromD,toD:t.toD,sex:t.sex})),
      tags:of('tags').filter(t=>t.kind!=='group').map(t=>({id:t.id,name:t.name})),groups:of('tags').filter(t=>t.kind==='group').map(t=>({id:t.id,name:t.name}))};
  }
  const blank=()=>({name:'',code:'',breeds:[],sexes:[{name:'Female'},{name:'Male'}],stages:[],tags:[],groups:[]});
  function editorDraft(ids){
    if(ED&&ED.key===ids)return ED;
    let blocks;
    if(ids==='new')blocks=[blank()];
    else if(ids==='all')blocks=S.active('species').map(blockFrom);
    else blocks=ids.split(',').map(id=>S.get('species',id)).filter(Boolean).map(blockFrom);
    ED={key:ids,blocks,removed:[],err:{},dirty:false};
    return ED;
  }
  function chipField(bi,field,items,ph,tone){
    return `<div class="chipin" data-chipbox="${bi}" data-f="${field}">${items.map((x,i)=>`<span class="tag t-${tone||'ok'}">${esc(x.name)}<button data-a="ed-chipdel" data-b="${bi}" data-f="${field}" data-i="${i}" aria-label="Remove ${esc(x.name)}">×</button></span>`).join('')}
      <input data-chipin="${bi}" data-f="${field}" placeholder="${esc(ph)}" aria-label="${esc(ph)}"></div>`;
  }
  function editorPage(ids){
    const d=editorDraft(ids); const E=d.err;
    const er=k=>E[k]?`<div class="ferr">${esc(E[k])}</div>`:'';
    return `<div class="phead"><div><div class="crumb">Configuration / <a href="#/configuration/items/species">Items and settings</a> / <b>Animal types</b></div><h1>${ids==='new'?'New animal types':'Edit animal types'}</h1></div><div class="sp"></div>
      <button class="btn" data-a="ed-cancel">Cancel</button><button class="btn p" data-a="ed-save">${ic('check')}Save ${d.blocks.length} species</button></div>
      <div class="stack">${d.blocks.map((b,bi)=>`<section class="spblk">
        <div class="hd"><div style="flex:1;min-width:160px"><input class="inl" style="width:100%;font-size:15px;font-weight:700" data-ed="${bi}" data-k="name" value="${esc(b.name)}" placeholder="Species" aria-label="Species name">${er(bi+'.name')}</div>
          <input class="inl" style="width:110px" data-ed="${bi}" data-k="code" value="${esc(b.code)}" placeholder="Code" aria-label="Code">
          <button class="btn sm" data-a="ed-copystages" data-b="${bi}">${ic('copy')}Copy stages</button>
          <button class="btn icon gh" data-a="ed-remove" data-b="${bi}" aria-label="Remove species">${ic('x')}</button></div>
        <div class="bd"><div class="fgrid">
          <div class="fld full"><label>Breeds <span class="muted">${b.breeds.length}</span></label>${chipField(bi,'breeds',b.breeds,'Add breed')}</div>
          <div class="fld"><label>Sexes</label>${chipField(bi,'sexes',b.sexes,'Add sex','info')}</div>
          <div class="fld"><label>Shed tags</label>${chipField(bi,'tags',b.tags,'Add shed tag','mut')}</div>
          <div class="fld"><label>Groups</label>${chipField(bi,'groups',b.groups,'Add group','pur')}</div>
          <div class="fld full"><label>Lifecycle stages <span class="muted">${b.stages.length}</span></label>${er(bi+'.stages')}
            <div class="twrap"><table class="mini" style="min-width:600px"><thead><tr><th style="width:110px">Code</th><th>Stage</th><th style="width:100px">From (days)</th><th style="width:100px">To (days)</th><th style="width:110px">Sex</th><th style="width:40px"></th></tr></thead><tbody>
            ${b.stages.map((s,si)=>`<tr><td><input data-es="${bi}" data-s="${si}" data-k="code" value="${esc(s.code)}" aria-label="Code" class="${E[bi+'.s'+si]?'bad':''}"></td><td><input data-es="${bi}" data-s="${si}" data-k="name" value="${esc(s.name)}" aria-label="Stage"></td>
              <td><input data-es="${bi}" data-s="${si}" data-k="fromD" value="${esc(s.fromD)}" inputmode="numeric" aria-label="From days"></td><td><input data-es="${bi}" data-s="${si}" data-k="toD" value="${esc(s.toD)}" inputmode="numeric" aria-label="To days" class="${E[bi+'.r'+si]?'bad':''}"></td>
              <td><select data-es="${bi}" data-s="${si}" data-k="sex" aria-label="Sex">${['any','female','male'].map(x=>`<option ${s.sex===x?'selected':''}>${x}</option>`).join('')}</select></td>
              <td class="x"><button class="btn icon gh" data-a="ed-stagedel" data-b="${bi}" data-s="${si}" aria-label="Remove stage">${ic('x')}</button></td></tr>`).join('')}
            </tbody></table></div>
            <button class="btn sm mt" data-a="ed-stageadd" data-b="${bi}">${ic('plus')}Stage</button></div>
        </div></div></section>`).join('')}
        <button class="btn out" data-a="ed-addsp">${ic('plus')}Add species</button></div>`;
  }
  function editorMount(root){
    const d=ED; if(!d)return;
    root.querySelectorAll('[data-ed]').forEach(i=>i.oninput=()=>{d.blocks[+i.dataset.ed][i.dataset.k]=i.value;d.dirty=true;});
    root.querySelectorAll('[data-es]').forEach(i=>i.oninput=i.onchange=()=>{d.blocks[+i.dataset.es].stages[+i.dataset.s][i.dataset.k]=i.value;d.dirty=true;});
    root.querySelectorAll('[data-chipin]').forEach(i=>{
      const refocus=()=>setTimeout(()=>{const n=document.querySelector(`[data-chipin="${i.dataset.chipin}"][data-f="${i.dataset.f}"]`);if(n)n.focus();},0);
      const add=()=>{const vals=i.value.split(/[,;\n\t]/).map(s=>s.trim()).filter(Boolean);if(!vals.length)return;const b=d.blocks[+i.dataset.chipin],f=i.dataset.f;
        vals.forEach(v=>{if(!b[f].some(x=>REG.norm(x.name)===REG.norm(v)))b[f].push({name:v});});d.dirty=true;App.render();refocus();};
      i.onkeydown=e=>{if(e.key==='Enter'||e.key===','){e.preventDefault();add();}else if(e.key==='Backspace'&&!i.value){d.blocks[+i.dataset.chipin][i.dataset.f].pop();App.render();refocus();}};
      i.onpaste=e=>{const t=(e.clipboardData||window.clipboardData).getData('text');if(/[\n,;\t]/.test(t)){e.preventDefault();i.value=t;add();}};
      i.onblur=()=>{if(i.value.trim())add();};
    });
  }
  Object.assign(A,{
    'ed-chipdel'(el){ED.blocks[+el.dataset.b][el.dataset.f].splice(+el.dataset.i,1);ED.dirty=true;App.render();},
    'ed-stageadd'(el){ED.blocks[+el.dataset.b].stages.push({code:'',name:'',fromD:'',toD:'',sex:'any'});ED.dirty=true;App.render();},
    'ed-stagedel'(el){ED.blocks[+el.dataset.b].stages.splice(+el.dataset.s,1);ED.dirty=true;App.render();},
    'ed-addsp'(){ED.blocks.push(blank());ED.dirty=true;App.render();},
    'ed-remove'(el){const b=ED.blocks.splice(+el.dataset.b,1)[0];if(b&&b.id)ED.removed.push(b.id);ED.dirty=true;App.render();},
    'ed-cancel'(){App.leave('#/configuration/items/species');},
    'ed-copystages'(el){const bi=+el.dataset.b;const srcs=ED.blocks.map((b,i)=>({b,i})).filter(x=>x.i!==bi&&x.b.name&&x.b.stages.length);
      S.active('species').forEach(s=>{if(!srcs.some(x=>x.b.id===s.id))srcs.push({b:blockFrom(s)});});
      UI.menu(el,srcs.length?srcs.map(x=>({label:x.b.name,icon:'copy',run(){const t=ED.blocks[bi];
        x.b.stages.forEach(s=>{if(!t.stages.some(y=>REG.norm(y.code)===REG.norm(s.code)))t.stages.push({code:s.code,name:s.name,fromD:s.fromD,toD:s.toD,sex:s.sex});});
        x.b.sexes.forEach(s=>{if(!t.sexes.some(y=>REG.norm(y.name)===REG.norm(s.name)))t.sexes.push({name:s.name});});ED.dirty=true;App.render();}})):[{label:'No stages to copy',run(){}}]);},
    'ed-save'(){
      const d=ED; d.err={};
      const ids=new Set(d.blocks.map(b=>b.id).filter(Boolean));
      d.blocks.forEach((b,bi)=>{
        const nm=REG.norm(b.name);
        if(!nm)d.err[bi+'.name']='Required';
        else if(d.blocks.some((o,oi)=>oi!==bi&&REG.norm(o.name)===nm))d.err[bi+'.name']='Duplicate';
        else if(S.all('species').some(s=>s.id!==b.id&&!ids.has(s.id)&&REG.norm(s.name)===nm))d.err[bi+'.name']='Already exists';
        const codes={};
        b.stages.forEach((s,si)=>{const c=REG.norm(s.code||s.name);if(!c||codes[c]){d.err[bi+'.s'+si]=1;d.err[bi+'.stages']='Stage codes must be unique';}codes[c]=1;
          if(s.fromD!==''&&s.toD!==''&&!isNaN(s.fromD)&&!isNaN(s.toD)&&Number(s.toD)<Number(s.fromD)){d.err[bi+'.r'+si]=1;d.err[bi+'.stages']='To is before From';}
          if((s.fromD!==''&&(isNaN(s.fromD)||Number(s.fromD)<0))||(s.toD!==''&&(isNaN(s.toD)||Number(s.toD)<0))){d.err[bi+'.r'+si]=1;d.err[bi+'.stages']='Days must be 0 or more';}});
      });
      if(Object.keys(d.err).length){App.render();UI.toast('Fix highlighted fields');return;}
      const n=S.snap('Saved animal types');
      d.removed.forEach(id=>{const s=S.get('species',id);if(s)s.status='archived';});
      d.blocks.forEach(b=>{
        let sp=b.id?S.get('species',b.id):null;
        if(sp)Object.assign(sp,{name:b.name.trim(),code:b.code.trim()||b.name.trim().toLowerCase(),status:'active'});
        else sp=S.add('species',{name:b.name.trim(),code:b.code.trim()||b.name.trim().toLowerCase()});
        const sync=(coll,items,attrs,filter)=>{
          const existing=S.all(coll).filter(x=>x.speciesId===sp.id&&x.status!=='archived'&&(!filter||filter(x)));
          const keep=new Set();
          items.forEach(it=>{const a=Object.assign({speciesId:sp.id},attrs(it));let rec=it.id?S.get(coll,it.id):existing.find(x=>REG.norm(x.name)===REG.norm(a.name)&&!keep.has(x.id));
            if(!rec)rec=S.all(coll).find(x=>x.speciesId===sp.id&&x.status==='archived'&&REG.norm(x.name)===REG.norm(a.name)&&(!filter||filter(x)));
            if(rec){Object.assign(rec,a);rec.status='active';}else rec=S.add(coll,a);keep.add(rec.id);});
          existing.forEach(x=>{if(!keep.has(x.id))x.status='archived';});
        };
        sync('breeds',b.breeds,it=>({name:it.name}));
        sync('sexes',b.sexes,it=>({name:it.name}));
        sync('tags',b.tags,it=>({name:it.name,kind:'tag'}),x=>x.kind!=='group');
        sync('tags',b.groups,it=>({name:it.name,kind:'group'}),x=>x.kind==='group');
        sync('stages',b.stages,it=>({code:(it.code||it.name).trim().toUpperCase(),name:(it.name||it.code).trim(),fromD:it.fromD===''?'':Number(it.fromD),toD:it.toD===''?'':Number(it.toD),sex:it.sex||'any'}));
      });
      S.save(); ED=null; App.leave('#/configuration/items/species',true);
      UI.toast('Saved '+d.blocks.length+' species',()=>{S.undoTo(n);App.render();});
    }
  });

  /* ---------- workbook menu ---------- */
  A['wb-menu']=el=>UI.menu(el,[
    {label:'Import workbook',icon:'upload',run:()=>{IO.start('workbook');}},
    {label:'Template (.xlsx)',icon:'download',run:()=>IO.workbookTemplate('xlsx')},
    {label:'Export all (.xlsx)',icon:'download',run:()=>IO.workbookTemplate('xlsx',true)}]);

  /* unsaved-draft guard for park page and animal types editor */
  P.dirty=function(){
    if(PD&&(PD.dirty||PD.created))return {discard(){if(PD.created){S.state=JSON.parse(PD.snap);S.save();}PD=null;}};
    if(ED&&ED.dirty)return {discard(){ED=null;}};
    const g=Sheet.cur; if(g&&location.hash.includes('/sheet/')&&g.rows.some(r=>!REG.isBlankRow(REG.R[g.regKey],r.v))&&!g.summary)return {discard(){Sheet.cur=null;}};
    return null;
  };

  /* ---------- router entry ---------- */
  P.render=function(parts){
    let key=parts[0]||'parks';
    const legacy={places:parts[1]||'parks','animal-types':parts[1]&&!['edit'].includes(parts[1])?parts[1]:'species',animals:'animals',items:'items',people:parts[1]||'people',vendors:parts[1]||'vendors',settings:'settings'};
    Sheet.activeGetter=null;
    if(key==='import')return {html:IO.page(),mount:IO.mount};
    if(key==='sheet'){
      const k=parts[1]; if(!REG.R[k])return {html:'<div class="empty">Not found</div>'};
      if(!Sheet.cur||Sheet.cur.regKey!==k)Sheet.cur=Sheet.make(k,[{}],{title:'Add '+REG.R[k].label.toLowerCase()});
      const g=Sheet.cur;
      return {html:`<div class="phead"><div><div class="crumb">Configuration / <a href="${g.back&&!g.back.includes('/sheet/')?g.back:'#/configuration/items/'+k}">Items and settings</a> / <b>${esc(REG.R[k].label)}</b></div><h1>${esc(g.title||REG.R[k].label)}</h1></div><div class="sp"></div>
        <button class="btn sm" data-a="io-menu" data-reg="${k}">${ic('sheet')}Import / export</button></div>${Sheet.html(g)}`,
        mount:root=>{const el=root.querySelector('[data-sheet]');if(el)Sheet.bind(el,()=>Sheet.cur,()=>App.render());}};
    }
    if(key==='park'||(key==='places'&&parts[1]==='park'))return {html:parkPage((key==='park'?parts[1]:parts[2])||'new'),mount:parkMount};
    if(key==='animal-types'&&parts[1]&&(parts[1]==='new'||parts[1]==='all'||parts[1]==='edit'||S.get('species',parts[1].split(',')[0]))){const ids=parts[1]==='edit'?(parts[2]||'new'):parts[1];return {html:editorPage(ids),mount:editorMount};}
    if(legacy[key]&&!PANELS[key])key=legacy[key];
    if(!PANELS[key])key='parks';
    PD=null; ED=null;
    return {html:header()+`<div class="cfg">${rail(key)}<div class="cfgpanel">${PANELS[key]()}</div></div>`};
  };
  window.SetupPage=P;
})();
