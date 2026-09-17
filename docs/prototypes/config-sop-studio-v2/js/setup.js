/* Configuration / Items and settings: register rail + register panel, park page, animal types editor */
(function(){
  const P={};
  const SECTIONS=[
    ['Farm places',[['parks','Parks'],['pens','Pens'],['partitions','Partitions'],['farms','Farms']]],
    ['Animal types',[['species','Species'],['breeds','Breeds'],['sexes','Sexes'],['stages','Lifecycle stages'],['groups','Pen tags'],['healthStates','Health states']]],
    ['Animals',[['animals','Animals']]],
    ['Catalogue',[['items','Items & categories'],['inventory','Inventory']]],
    ['People',[['people','People'],['roles','Roles']]],
    ['Reference lists',[['statusDefs','Status definitions'],['exitReasons','Exit reasons'],['purposes','Animal purposes'],['movementReasons','Movement reasons'],['weightBands','Weight bands'],['sopCategories','SOP categories'],['taskTypes','Task types']]],
    ['Business rules',[['saleProducts','Sale product types'],['costKinds','Cost kinds'],['identifierPolicies','Identifier policies'],['designations','Designations'],['approvalChains','Approval chains']]],
    ['Business settings',[['settings','Business settings']]]
  ];
  const LINKS=[['Vendors','#/procurement/vendors'],['Market cities','#/sales/config'],['Diseases','#/health/config'],['Ration groups','#/feed/config'],['Vaccination plan','#/vaccination/plan']];
  const regOf=k=>k==='shed-tags'||k==='groups'?'tags':k;
  const count=k=>{if(k==='inventory')return S.active('items').filter(i=>REG.on(i.trackStock)).length;if(k==='groups')return S.active('tags').length;if(k==='identifierPolicies'&&!REG.R[k]){const v=S.active(k)[0];return v?(v.types||[]).length:0;}const r=REG.R[regOf(k)];return r?S.active(r.coll).length:S.active(k).length;};
  const penDisplay=a=>{const pen=S.get('pens',a.penId),pt=S.get('partitions',a.partitionId);return pen?REG.label('pens',pen)+(pt?' - '+pt.name:''):'';};
  const num=v=>`<span class="num">${esc(v)}</span>`;

  function header(){
    return `<div class="phead"><div><div class="crumb">Configuration / <b>Items and settings</b></div><h1>Items and settings</h1></div><div class="sp"></div>
      <button class="btn out" data-a="wb-menu">${ic('sheet')}Farm setup workbook</button></div>`;
  }
  function rail(active){
    const sec=SECTIONS.find(s=>s[1].some(x=>x[0]===active));
    return `<nav class="cfgrail card" aria-label="Registers">${SECTIONS.map(([t,items])=>`<div class="rsec"><div class="eyebrow">${esc(t)}</div>${items.map(([k,l])=>
        `<a class="ritem ${k===active?'on':''}" href="#/configuration/items/${k}"><span>${esc(l)}</span><span class="rct">${count(k)}</span></a>`).join('')}</div>`).join('')}<div class="rsec"><div class="eyebrow">Other modules</div>${LINKS.map(([l,h])=>`<a class="ritem" href="${h}"><span>${l}</span><span class="rct">${ic('chevron-right','',12)}</span></a>`).join('')}</div></nav>
      <select class="fsel cfgrail-m" data-railsel aria-label="Register">${SECTIONS.map(([t,items])=>`<optgroup label="${esc(t)}">${items.map(([k,l])=>`<option value="${k}" ${k===active?'selected':''}>${esc(l)} (${count(k)})</option>`).join('')}</optgroup>`).join('')}<optgroup label="Other modules">${LINKS.map(([l,h])=>`<option value="${h}">${l}</option>`).join('')}</optgroup></select>`;
  }
  document.addEventListener('change',e=>{if(e.target.matches&&e.target.matches('[data-railsel]'))location.hash=e.target.value.startsWith('#')?e.target.value:'#/configuration/items/'+e.target.value;});

  function list(regKey,opts){opts=opts||{};List.opts[regKey]=opts;return List.html(regKey,opts);}

  const PANELS={
    parks:()=>list('parks',{eyebrow:'Farm places',filters:['farm'],onOpen:id=>location.hash='#/configuration/items/park/'+id,onNew:()=>location.hash='#/configuration/items/park/new',addLabel:'Add park',
      columns:[{label:'Park',html:r=>`<b>${esc(r.name)}</b>`},{label:'Code',w:150,html:r=>`<span class="mono">${esc(r.code||'')}</span>`},{label:'Farm',html:r=>esc(REG.labelById('farms',r.farmId))},
        {label:'Pens',w:80,num:1,html:r=>S.active('pens').filter(p=>p.parkId===r.id).length},{label:'Capacity',w:100,num:1,html:r=>{const t=S.active('pens').filter(p=>p.parkId===r.id).reduce((a,p)=>a+(+p.capacity||0),0);return t||'—';}},
        {label:'Animals',w:90,num:1,html:r=>S.active('animals').filter(a=>a.parkId===r.id).length}]}),
    pens:()=>list('pens',{eyebrow:'Farm places',filters:['park','stage','sex'],groupBy:r=>REG.labelById('parks',r.parkId)||'No park',
      sort:(a,b)=>(REG.labelById('parks',a.parkId)||'\uffff').localeCompare(REG.labelById('parks',b.parkId)||'\uffff')||REG.label('pens',a).localeCompare(REG.label('pens',b),undefined,{numeric:true}),
      columns:[{label:'Pen',html:r=>`<b>${esc(REG.label('pens',r))}</b>`},{label:'Partitions',w:100,num:1,html:r=>S.active('partitions').filter(p=>p.penId===r.id).length},
        {label:'Animals',w:90,num:1,html:r=>S.active('animals').filter(a=>a.penId===r.id).length},{label:'Capacity',w:90,num:1,html:r=>{const n=S.active('animals').filter(a=>a.penId===r.id).length;return (r.capacity===''||r.capacity==null?'—':esc(r.capacity))+(r.capacity&&n>r.capacity?' '+UI.tag('Over','dng'):'');}},
        {label:'Stage',html:r=>esc(REG.labelById('stages',r.stageId))},{label:'Sex',w:90,html:r=>esc(REG.enumLabel(r.sex))}]}),
    partitions:()=>list('partitions',{eyebrow:'Farm places',filters:['park','pen'],
      columns:[{label:'Partition',html:r=>`<b>${esc(REG.fullLabel('partitions',r))}</b>`},{label:'Park',html:r=>{const p=S.get('pens',r.penId);return esc(p?REG.labelById('parks',p.parkId):'');}},
        {label:'Capacity',w:100,num:1,html:r=>esc(r.capacity===''||r.capacity==null?'—':r.capacity)},{label:'Animals',w:90,num:1,html:r=>S.active('animals').filter(a=>a.partitionId===r.id).length}]}),
    farms:()=>list('farms',{eyebrow:'Farm places',columns:[{label:'Code',w:90,html:r=>`<b class="mono">${esc(r.code)}</b>`},{label:'Farm',html:r=>esc(r.name)},{label:'Type',w:110,html:r=>esc(REG.enumLabel(r.kind))},{label:'Parks',w:80,num:1,html:r=>S.active('parks').filter(p=>p.farmId===r.id).length}]}),
    species:()=>list('species',{eyebrow:'Animal types',onOpen:id=>location.hash='#/configuration/items/animal-types/'+id,onNew:()=>location.hash='#/configuration/items/animal-types/new',addLabel:'Add species',
      extraBtns:`<a class="btn sm" href="#/configuration/items/animal-types/all">${ic('edit-3')}Edit in grid</a>`,
      columns:[{label:'Species',html:r=>`<b>${esc(r.name)}</b>`},{label:'Code',w:90,html:r=>`<span class="mono">${esc(r.code||'')}</span>`},
        ...['breeds','stages'].map(c=>({label:REG.R[c].label.replace('Lifecycle ',''),w:90,num:1,html:r=>S.active(c).filter(x=>x.speciesId===r.id).length})),
        {label:'Animals',w:90,num:1,html:r=>S.active('animals').filter(a=>a.speciesId===r.id).length}]}),
    breeds:()=>list('breeds',{eyebrow:'Animal types',filters:['species']}),
    sexes:()=>list('sexes',{eyebrow:'Animal types',filters:[]}),
    stages:()=>list('stages',{eyebrow:'Animal types',filters:['species','sex'],columns:[
      {label:'Stage',html:r=>`<b>${esc(r.name)}</b>`},{label:'Code',w:100,html:r=>`<span class="mono">${esc(r.code)}</span>`},{label:'Species',w:110,html:r=>esc(REG.labelById('species',r.speciesId))},
      {label:'From (days)',w:110,num:1,html:r=>esc(r.fromD===''||r.fromD==null?'—':r.fromD)},{label:'To (days)',w:100,num:1,html:r=>esc(r.toD===''||r.toD==null?'—':r.toD)},{label:'Sex',w:90,html:r=>esc(REG.enumLabel(r.sex))}]}),
    'shed-tags':()=>list('tags',{eyebrow:'Animal types',title:'Pen tags',filters:['species'],where:r=>r.kind!=='group',defaults:()=>({kind:'tag'}),addLabel:'Add pen tag'}),
    groups:()=>list('tags',{eyebrow:'Animal types',title:'Pen tags',filters:['species','kind'],addLabel:'Add pen tag'}),
    healthStates:()=>list('healthStates',{eyebrow:'Animal types',filters:['group']}),
    animals:()=>list('animals',{eyebrow:'Animals',filters:['species','park','pen','stage','sex'],addLabel:'Add animals',onNew:()=>Sheet.openEntry('animals',null,[{}]),
      extraBtns:`<button class="btn sm" data-a="rec-one" data-reg="animals">${ic('plus')}Add one</button>`,
      columns:[{label:'RFID',html:r=>`<b class="mono">${esc(r.rfid)}</b>`},{label:'Second tag',low:1,html:r=>`<span class="muted">${esc(r.tag2||'')}</span>`},
        {label:'Species',html:r=>esc(REG.labelById('species',r.speciesId))},{label:'Breed',html:r=>esc(REG.labelById('breeds',r.breedId))},
        {label:'Sex',w:80,low:1,html:r=>esc(REG.labelById('sexes',r.sexId))},{label:'Stage',html:r=>esc(REG.labelById('stages',r.stageId))},
        {label:'Park',low:1,html:r=>esc(REG.labelById('parks',r.parkId))},{label:'Pen',html:r=>esc(penDisplay(r))},{label:'Weight (kg)',w:100,num:1,low:1,html:r=>esc(r.weight===''||r.weight==null?'':r.weight)}]}),
    items:()=>items(),
    inventory:()=>inventory(),
    people:()=>list('people',{eyebrow:'People',filters:['role','parks'],columns:[{label:'Name',html:r=>`<b>${esc(/\s0\d$/.test(r.name||'')?REG.labelById('roles',r.roleId):r.name)}</b>`},{label:'Role',html:r=>esc(REG.labelById('roles',r.roleId))},
      {label:'Parks',html:r=>(r.parkIds||[]).length?(r.parkIds||[]).map(id=>UI.tag(REG.labelById('parks',id),'mut')).join(' '):'<span class="muted">All parks</span>'},{label:'Email',html:r=>esc(r.email||'')},{label:'Phone',html:r=>esc(r.phone||'')}]}),
    roles:()=>list('roles',{eyebrow:'People',columns:[{label:'Role',html:r=>`<b>${esc(r.name)}</b>`},{label:'Grade',w:140,html:r=>esc(r.grade||'')},{label:'People',w:90,num:1,html:r=>{const n=S.active('people').filter(p=>p.roleId===r.id).length;return n||'<span class="muted">—</span>';}}]}),
    settings:()=>list('settings',{eyebrow:'Business settings',filters:['dept'],onOpen:id=>settingDrawer(id),
      where:r=>{const st=List.st.settings;const q=st&&REG.norm(st.q);return !q||REG.norm(r.name).includes(q);},
      columns:[{label:'Setting',html:r=>`<b>${esc(r.name)}</b>`},{label:'Department',w:160,html:r=>UI.tag(r.dept,'teal')},
        {label:'Value',w:200,html:r=>`<div class="valcell"><input class="inl num" data-setval="${r.id}" value="${esc(r.value)}" aria-label="${esc(r.name)}" ${r.type==='time'?'':'inputmode="decimal"'}><span class="u">${esc(r.unit||'')}</span></div>`}]})
      +`<section class="card mt"><div class="hd"><h3>Module configuration</h3></div>${[['Vaccination plan','#/vaccination/plan','Preventive Care'],['Feed Config','#/feed/config','Feed'],['Health Config','#/health/config','Health'],['Sales Config','#/sales/config','Sales']]
        .map(([l,h,dp])=>`<a class="linkrow" href="${h}"><b>${l}</b><span class="tag t-teal">${dp}</span><span class="sp"></span>${ic('chevron-right')}</a>`).join('')}</section>`
  };
  ['statusDefs','exitReasons','purposes','movementReasons','weightBands','sopCategories','taskTypes'].filter(k=>REG.R[k])
    .forEach(k=>PANELS[k]=()=>list(k,{eyebrow:'Reference lists',filters:REG.R[k].cols.filter(c=>c.type==='enum'||c.type==='ref').map(c=>c.k).slice(0,2)}));

  /* business rules that were code constants: A's register when it exists, else a read-only table from the seed */
  const roTable=(title,heads,rows)=>`<section class="card" data-ro="${esc(title)}"><div class="hd lhd"><div class="ttl"><div class="eyebrow">Business rules</div><h3>${esc(title)} <span class="cnt">${rows.length}</span></h3></div><span class="sp"></span>${UI.tag('Read only','mut')}</div>
    <div class="twrap screen ltable"><table class="ltbl"><thead><tr>${heads.map(h=>`<th class="${h[1]?'num':''}">${esc(h[0])}</th>`).join('')}</tr></thead>
    <tbody>${rows.length?rows.map(r=>`<tr>${r.map((c,i)=>`<td class="${heads[i][1]?'num':''}" data-label="${esc(heads[i][0])}">${c}</td>`).join('')}</tr>`).join(''):`<tr><td colspan="${heads.length}" class="empty">None</td></tr>`}</tbody></table></div></section>`;
  const yes=v=>v?UI.tag('Yes','ok'):`<span class="muted">No</span>`;
  const RULES={
    saleProducts:()=>roTable('Sale product types',[['Product'],['Species']],S.active('saleProducts').map(r=>[`<b>${esc(r.name)}</b>`,esc(r.speciesId?REG.labelById('species',r.speciesId):'')])),
    costKinds:()=>roTable('Cost kinds',[['Cost kind'],['Key']],S.active('costKinds').map(r=>[`<b>${esc(r.name)}</b>`,`<span class="mono">${esc(r.code)}</span>`])),
    identifierPolicies:()=>{const v=S.active('identifierPolicies')[0]||{types:[]};return roTable('Identifier policies'+(v.version?' · v'+v.version:''),[['Identifier'],['Key'],['Primary allowed'],['Auto link']],(v.types||[]).map(t=>[`<b>${esc(t.name)}</b>`,`<span class="mono">${esc(t.code)}</span>`,yes(t.primaryAllowed),yes(t.autoLink)]));},
    designations:()=>roTable('Designations',[['Designation'],['Key'],['Grade']],S.active('designations').map(r=>[`<b>${esc(r.name)}</b>`,`<span class="mono">${esc(r.code)}</span>`,esc(REG.enumLabel(r.grade||''))])),
    approvalChains:()=>roTable('Approval chains',[['Department'],['Request'],['Approvers']],S.active('approvalChains').map(r=>[UI.tag(r.dept,'teal'),`<b>${esc(r.name)}</b>`,(r.steps||[]).map(st=>UI.tag(st.role?REG.enumLabel(st.role)+(st.perPerson?' · per person':''):(S.active('designations').find(d=>d.code===st.designation)||{name:REG.enumLabel(st.designation)}).name,'mut')).join(' ')]))
  };
  Object.keys(RULES).forEach(k=>PANELS[k]=()=>REG.R[k]?list(k,{eyebrow:'Business rules'}):RULES[k]());

  /* settings drawer: name + department fixed; value, unit, effective date editable */
  function settingDrawer(id){
    const s=S.get('settings',id); if(!s)return;
    const f={value:s.value==null?'':String(s.value),unit:s.unit||'',eff:s.effectiveFrom||''};
    UI.drawer({title:s.name,sub:s.dept,
      body:`<div class="fgrid"><div class="fld"><label>Setting</label><input value="${esc(s.name)}" readonly aria-readonly="true" tabindex="-1" style="background:transparent;border-style:dashed;color:var(--muted)"></div>
        <div class="fld"><label>Department</label><input value="${esc(s.dept)}" readonly aria-readonly="true" tabindex="-1" style="background:transparent;border-style:dashed;color:var(--muted)"></div>
        <div class="fld"><label>Value *</label><input data-sd="value" value="${esc(f.value)}" ${s.type==='time'?'placeholder="HH:MM"':'inputmode="decimal"'}></div>
        <div class="fld"><label>Unit</label><input data-sd="unit" value="${esc(f.unit)}"></div>
        <div class="fld"><label>Effective from</label><input data-sd="eff" value="${esc(f.eff)}" placeholder="DD/MM/YYYY"></div>
        <div class="fld full" data-sderr></div></div>`,
      foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-sdsave>Save</button>`,
      mount(dr){
        dr.querySelectorAll('[data-sd]').forEach(i=>i.oninput=()=>{f[i.dataset.sd]=i.value;});
        const err=t=>{dr.querySelector('[data-sderr]').innerHTML=t?`<div class="ferr">${esc(t)}</div>`:'';};
        dr.querySelector('[data-sdsave]').onclick=()=>{
          const raw=f.value.trim(),eff=f.eff.trim();
          if(s.type==='time'?!/^\d{1,2}:\d{2}$/.test(raw):(raw===''||isNaN(Number(raw))))return err(s.type==='time'?'Value: HH:MM':'Value: number');
          if(eff&&!/^\d{2}\/\d{2}\/\d{4}$/.test(eff))return err('Effective from: DD/MM/YYYY');
          UI.closeDrawer();
          UI.undoable('Saved '+s.name,()=>{s.value=s.type==='time'?raw:Number(raw);s.unit=f.unit.trim();if(eff)s.effectiveFrom=eff;else delete s.effectiveFrom;App.render();});
        };}});
  }

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

  /* ---------- items & catalogues ---------- */
  let catSel='',catQ='',catArch=false;
  const subtree=id=>REG.catSubtree(id);
  const isRoot=c=>!c.parentId||!S.get('categories',c.parentId);
  const DEPT_OPTS=()=>REG.R.items.cols.find(c=>c.k==='depts').opts;
  function items(){
    const cats=S.all('categories').filter(c=>catArch?c.status==='archived':c.status!=='archived');
    const q=REG.norm(catQ);
    const show=c=>!q||[...subtree(c.id)].some(id=>{const x=S.get('categories',id);return x&&REG.norm(x.name).includes(q);});
    const node=(c,depth)=>{if(!show(c))return '';const kids=cats.filter(x=>(x.parentId||'')===c.id);const n=S.active('items').filter(i=>subtree(c.id).has(i.categoryId)).length;
      return `<div class="tn ${catSel===c.id?'on':''}" style="padding-left:${8+depth*16}px" data-a="cat-sel" data-id="${c.id}"><span class="tw">${ic(c.system?'lock':kids.length?'folder':'circle','',13)}</span><span class="tnl">${esc(c.name)}</span><span class="ct">${n}</span>
        <button class="btn icon" data-a="cat-menu" data-id="${c.id}" aria-label="List actions">${ic('more','',15)}</button></div>${kids.map(k=>node(k,depth+1)).join('')}`;};
    const roots=catArch?cats.filter(c=>!c.parentId||!cats.some(x=>x.id===c.parentId)):cats.filter(c=>!c.parentId);
    const sys=roots.filter(c=>c.system),mine=roots.filter(c=>!c.system);
    const where=catSel?(r=>subtree(catSel).has(r.categoryId)):null;
    const selCat=catSel&&S.get('categories',catSel);
    List.opts.items={eyebrow:selCat?(selCat.system||REG.catRoot(catSel).system?'Catalogue':'List'):'',title:selCat?selCat.name:'Items',where,filters:['depts'],defaults:()=>catSel?{category:REG.catPath(catSel)}:{},
      onOpen:id=>itemForm(id),onNew:()=>itemForm(null)};
    window.ItemsKind=selCat?REG.itemKindOfCat(catSel):'';window.ItemsCat=selCat?catSel:'';
    return `<div class="split"><section class="card"><div class="hd"><h3>Lists</h3><span class="cnt">${S.active('categories').filter(isRoot).length}</span></div>
      <div class="catpick"><select data-catpick aria-label="List"><option value="">All items · ${S.active('items').length}</option>${(()=>{const walk=(c,d)=>[`<option value="${c.id}" ${catSel===c.id?'selected':''}>${'\u00a0\u00a0'.repeat(d)+esc(c.name)} · ${S.active('items').filter(i=>subtree(c.id).has(i.categoryId)).length}</option>`].concat(cats.filter(x=>(x.parentId||'')===c.id).map(k=>walk(k,d+1))).flat();
          return (sys.length?`<optgroup label="Catalogues">${sys.map(c=>walk(c,0).join('')).join('')}</optgroup>`:'')+(mine.length?`<optgroup label="Your lists">${mine.map(c=>walk(c,0).join('')).join('')}</optgroup>`:'');})()}</select>
        ${catSel?`<button class="btn icon" data-a="cat-menu" data-id="${catSel}" aria-label="List actions">${ic('more','',15)}</button>`:''}<button class="btn sm" data-a="cat-arch" data-v="${catArch?'':'1'}">${catArch?'Active':'Archived'}</button>${catArch?'':`<button class="btn sm" data-a="list-new">${ic('plus')}New list</button>`}</div>
      <div class="lbar"><label class="search">${ic('search')}<input data-catq value="${esc(catQ)}" placeholder="Search"></label>
        <div class="seg"><button class="${catArch?'':'on'}" data-a="cat-arch" data-v="">Active</button><button class="${catArch?'on':''}" data-a="cat-arch" data-v="1">Archived</button></div></div>
      ${catArch||!selCat?'':`<div class="lbar newcat"><input class="inl" style="flex:1" data-newcat placeholder="Subcategory" aria-label="New subcategory in ${esc(selCat.name)}"><button class="btn sm out" data-a="cat-add">${ic('plus')}Add</button></div>`}
      <div class="tree">${catArch?'':`<div class="tn ${catSel?'':'on'}" data-a="cat-sel" data-id=""><span class="tw">${ic('layers','',13)}</span><span class="tnl">All items</span><span class="ct">${S.active('items').length}</span></div>`}
      ${sys.length?`<div class="eyebrow" style="display:flex;align-items:center;gap:5px;padding:10px 8px 4px">${ic('lock','',12)}Catalogues</div>${sys.map(c=>node(c,0)).join('')}`:''}
      <div class="eyebrow" style="padding:10px 8px 4px">Your lists</div>${mine.map(c=>node(c,0)).join('')||(catArch?'<div class="empty">None</div>':'')}
      ${catArch?'':`<button class="btn sm gh" style="margin:4px 4px 6px" data-a="list-new">${ic('plus')}New list</button>`}</div></section>
      <div>${List.html('items',List.opts.items)}</div></div>`;
  }
  document.addEventListener('change',e=>{if(e.target.matches&&e.target.matches('[data-catpick]')){catSel=e.target.value;App.render();}});
  document.addEventListener('input',e=>{if(e.target.matches&&e.target.matches('[data-catq]')){catQ=e.target.value;const p=e.target.selectionStart;App.render();const n=document.querySelector('[data-catq]');if(n){n.focus();n.setSelectionRange(p,p);}}});

  /* SOP questions / module rules using a category: archive guard with a link */
  function catBlocked(c){const u=REG.itemUses(null,c.id,true).filter(x=>x.published);if(!u.length)return false;const f=u[0];
    UI.toast('Cannot archive',null,{label:'Used in '+f.label+(u.length>1?' +'+(u.length-1):''),link:true,run:()=>{location.hash=f.href;}});return true;}

  function newListDrawer(){
    const f={name:'',subs:'',depts:[]};let err='';
    const body=()=>`<div class="fgrid"><div class="fld full"><label>Name *</label><input data-nl="name" value="${esc(f.name)}">${err?`<div class="ferr">${esc(err)}</div>`:''}</div>
      <div class="fld full"><label>Subcategories</label><textarea data-nl="subs" rows="4" placeholder="One per line">${esc(f.subs)}</textarea></div>
      <div class="fld full"><label>Departments</label><div class="chkrow">${DEPT_OPTS().map(d=>`<label><input type="checkbox" data-nld value="${esc(d)}" ${f.depts.includes(d)?'checked':''}>${esc(d)}</label>`).join('')}</div></div></div>`;
    UI.drawer({title:'New list',body:body(),foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-nlsave>Create</button>`,
      mount(dr){const wire=()=>{dr.querySelectorAll('[data-nl]').forEach(i=>i.oninput=()=>{f[i.dataset.nl]=i.value;});
          dr.querySelectorAll('[data-nld]').forEach(i=>i.onchange=()=>{f.depts=[...dr.querySelectorAll('[data-nld]:checked')].map(x=>x.value);});};wire();
        dr.querySelector('[data-nlsave]').onclick=()=>{const nm=f.name.trim();
          err=!nm?'Required':S.active('categories').some(c=>!c.parentId&&REG.norm(c.name)===REG.norm(nm))?'Already exists':'';
          if(err){UI.redrawDrawer({body:body()});return;}
          UI.closeDrawer();UI.undoable('Created '+nm,()=>{const c=S.add('categories',{name:nm,parentId:'',kind:'general',system:false,depts:f.depts});
            [...new Set(f.subs.split(/\n|;/).map(x=>x.trim()).filter(Boolean))].forEach(x=>S.add('categories',{name:x,parentId:c.id}));catSel=c.id;catArch=false;App.render();});};}});
  }

  /* Inventory: every stock-tracked item in any catalogue or list; lots and movements live here, not on the item */
  const INV={q:'',cat:'',park:'',flag:''};
  function inventory(){
    const now=Date.now();
    const rows=S.active('items').filter(i=>REG.on(i.trackStock)).map(i=>{
      const lots=S.active('lots').filter(l=>l.itemId===i.id&&+l.qty>0&&(!INV.park||l.parkId===INV.park));
      const qty=lots.reduce((a,l)=>a+(+l.qty||0),0),exp=lots.map(l=>l.expiresOn).filter(Boolean).sort()[0]||'';
      const days=exp?Math.ceil((Date.parse(exp)-now)/864e5):null;
      const low=REG.lowStock(i,INV.park||''),expiring=days!=null&&REG.on(i.trackExpiry)&&days<=(+i.expiryAlertDays||0);
      return {i,lots,qty,exp,days,low,expiring,root:REG.catRoot(i.categoryId)};})
      .filter(r=>(!INV.cat||(r.root&&r.root.id===INV.cat))&&(!INV.flag||(INV.flag==='low'?r.low:r.expiring))&&(!INV.q||REG.norm(r.i.name).includes(REG.norm(INV.q))))
      .sort((a,b)=>(b.low+b.expiring)-(a.low+a.expiring)||a.i.name.localeCompare(b.i.name));
    const roots=S.active('categories').filter(isRoot);
    const tracked=S.active('items').filter(i=>REG.on(i.trackStock));
    return `<section class="card" data-inv><div class="hd lhd"><div class="ttl"><div class="eyebrow">Stock</div><h3>Inventory <span class="cnt">${tracked.length}</span></h3></div><span class="sp"></span>
        <button class="btn sm" data-a="inv-io">${ic('sheet')}Import / export</button><button class="btn sm p" data-a="inv-receive">${ic('plus')}Receive stock</button></div>
      <div class="lbar"><label class="search">${ic('search')}<input data-invq value="${esc(INV.q)}" placeholder="Search items"></label>
        <select class="fsel" data-inv="cat" aria-label="Catalogue or list"><option value="">Catalogue / list: all</option>${roots.map(c=>`<option value="${c.id}" ${INV.cat===c.id?'selected':''}>${esc(c.name)}</option>`).join('')}</select>
        <select class="fsel" data-inv="park" aria-label="Park"><option value="">Park: all</option>${S.active('parks').map(p=>`<option value="${p.id}" ${INV.park===p.id?'selected':''}>${esc(p.name)}</option>`).join('')}</select><span class="sp"></span>
        <div class="seg">${[['','All'],['low','Low stock'],['expiring','Expiring']].map(([k,l])=>`<button class="${INV.flag===k?'on':''}" data-a="inv-flag" data-v="${k}">${l}</button>`).join('')}</div></div>
      <div class="twrap screen ltable">${rows.length?`<table class="ltbl"><thead><tr><th>Item</th><th class="num">In stock</th><th>Lots</th><th>Nearest expiry</th><th>Alerts</th></tr></thead><tbody>
        ${rows.map(r=>`<tr class="clk" data-a="inv-open" data-id="${r.i.id}"><td data-label="Item"><b>${esc(r.i.name)}</b><div class="muted small">${esc([r.root&&r.root.name,REG.on(r.i.serialized)?'Serial numbers':'',r.i.unit].filter(Boolean).join(' · '))}</div></td>
          <td class="num" data-label="In stock">${r.lots.length?r.qty:'<span class="muted">0</span>'}</td>
          <td data-label="Lots">${r.lots.length?r.lots.length+' · '+esc([...new Set(r.lots.map(l=>REG.labelById('parks',l.parkId)))].join(', ')):'<span class="muted">No stock yet</span>'}</td>
          <td data-label="Nearest expiry">${r.exp?esc(fmtD(r.exp)):''}</td>
          <td data-label="Alerts">${[r.low?UI.tag(r.lots.length?'Low stock':'Out of stock','warn'):'',r.expiring?UI.tag(r.days<0?'Expired':'Expires in '+r.days+' d',r.days<0?'dng':'warn'):''].join(' ')}</td></tr>`).join('')}</tbody></table>`
        :tracked.length&&(INV.q||INV.cat||INV.park||INV.flag)?`<div class="empty">No items match</div>`:`<div class="empty">No stock yet<div class="mt"><button class="btn p" data-a="inv-receive">${ic('plus')}Receive stock</button></div></div>`}</div></section>`;
  }
  document.addEventListener('change',e=>{const t=e.target;if(t.matches&&t.matches('select[data-inv]')){INV[t.dataset.inv]=t.value;App.render();}});
  document.addEventListener('input',e=>{const t=e.target;if(t.matches&&t.matches('[data-invq]')){INV.q=t.value;const p=t.selectionStart;App.render();const n=document.querySelector('[data-invq]');if(n){n.focus();n.setSelectionRange(p,p);}}});
  Object.assign(A,{
    'inv-flag'(el){INV.flag=el.dataset.v;App.render();},
    'inv-open'(el){itemForm(el.dataset.id,{tab:'stock'});},
    'inv-io'(el){UI.menu(el,[{label:'Import stock receipts',icon:'upload',run:()=>IO.start('lots')},{label:'Stock receipts template (.xlsx)',icon:'download',run:()=>IO.template('lots','xlsx')},{label:'Stock receipts template (.csv)',icon:'download',run:()=>IO.template('lots','csv')}]);},
    'inv-receive'(){const its=S.active('items').filter(i=>REG.on(i.trackStock));
      UI.drawer({head:`<div class="ihead"><span class="ikic">${ic('package','',18)}</span><div class="ihead-t"><div class="icrumb">Inventory</div><h2>Receive stock</h2></div></div>`,cls:'idr',
        body:`<section class="isec"><div class="isec-b" style="border:0"><div class="fld full"><label for="rcv_item">Item<i class="rq"></i></label><select id="rcv_item" data-rcv class="ph" ${its.length?'':'disabled'}><option value="">${its.length?'Select item':'No stock-tracked items'}</option>${S.active('categories').filter(isRoot).map(c=>{const xs=its.filter(i=>{const r=REG.catRoot(i.categoryId);return r&&r.id===c.id;});return xs.length?`<optgroup label="${esc(c.name)}">${xs.map(i=>`<option value="${i.id}">${esc(i.name)}</option>`).join('')}</optgroup>`:'';}).join('')}</select></div></div></section>`,
        foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span>`,
        mount(dr){const sel=dr.querySelector('[data-rcv]');sel.onchange=e=>{if(e.target.value)itemForm(e.target.value,{tab:'stock',act:'receive'});};}});}
  });

  /* item drawer: definition + policy only (stock lives in lots); owner department locked on fixed catalogues */
  const KIND_FIELDS={medicine:['unit','route','strength','withdrawalDays','prescription'],vaccine:['disease','doseMl','vaccineType','boosterDays'],feed:['unit','dryMatter','protein'],general:['unit']};
  const KIND_ICON={medicine:'stethoscope',vaccine:'syringe',feed:'wheat',general:'folder'};
  const KIND_ONE={medicine:'medicine',vaccine:'vaccine',feed:'feed item',general:'item'};
  const SUFFIX={doseMl:'ml',boosterDays:'days',withdrawalDays:'days',dryMatter:'%',protein:'%',expiryAlertDays:'days',checkEvery:'days'};
  const LABEL={name:'Name',doseMl:'Dose',boosterDays:'Booster gap',withdrawalDays:'Withdrawal',dryMatter:'Dry matter',protein:'Protein',expiryAlertDays:'Alert before expiry',checkEvery:'Check every',reorderAt:'Reorder at',stockRole:'Alert role',expiryRole:'Alert role',checkRole:'Assigned role',vaccineType:'Live / killed',prescription:'Prescription needed',proof:'Proof'};
  const SEG={vaccineType:['live','killed'],prescription:['yes','no'],proof:['photo','video','none']};
  const countUnit=u=>/^(piece|pieces|pc|pcs|tag|tags|no|nos|unit|units|each|count)$/i.test(String(u||'').trim());
  const fmtD=d=>{const m=/^(\d{4})-(\d\d)-(\d\d)$/.exec(d||'');return m?m[3]+'/'+m[2]+'/'+m[1]:(d||'');};
  function itemForm(id,opt){
    opt=opt||{};
    const reg=REG.R.items,rec=id?S.get('items',id):null;
    const v=rec?REG.toRow('items',rec):{};reg.cols.forEach(c=>{if(v[c.k]==null)v[c.k]='';});
    Object.keys(REG.ITEM_CAPS).concat(['serialized']).forEach(k=>{v[k]=REG.on(v[k])?'yes':'no';});
    const rbp=Object.assign({},rec&&rec.reorderByPark||{});
    const recRoot=rec&&REG.catRoot(rec.categoryId);
    const recKind=rec?REG.itemKindOfCat(rec.categoryId):'';
    const roots=S.active('categories').filter(isRoot).concat(recRoot&&recRoot.status==='archived'?[recRoot]:[]).filter(c=>!rec||REG.itemKindOfCat(c.id)===recKind);
    const startCat=rec?rec.categoryId:(opt.cat!==undefined?opt.cat:catSel);const r0=startCat&&REG.catRoot(startCat);
    const f={act:opt.act?{kind:opt.act}:null,root:r0?r0.id:'',sub:startCat&&r0&&startCat!==r0.id?REG.catPath(startCat).split(' > ').slice(1).join(' > '):'',newSub:false,status:rec?rec.status:'active',tab:opt.tab||'details'};
    const deptList=()=>String(v.depts).split(/;\s*/).filter(Boolean);
    const owner=()=>f.root?REG.ownerDept(f.root):'';
    const setDepts=a=>{const o=owner();v.depts=(o?[o]:[]).concat(a.filter(x=>x!==o)).join('; ');};
    if(!rec&&r0)setDepts(r0.depts||[]);else setDepts(deptList());
    let shown=false,res=null;
    const col=k=>reg.cols.find(c=>c.k===k);
    const errOf=k=>{const i=shown&&res&&res.issues[k];return i&&i[0]==='err'?`<div class="ferr">${esc(i[1])}</div>`:'';};
    const lbl=(k,req)=>`<label for="if_${k}">${esc(LABEL[k]||col(k).label)}${req?'<i class="rq" title="Required" aria-label="required"></i>':''}</label>`;
    const field=(k,req,full)=>{const c=col(k);let input;const unit=k==='reorderAt'?(v.unit||''):SUFFIX[k];
      if(SEG[k])input=`<div class="seg iseg" role="radiogroup" aria-label="${esc(LABEL[k]||c.label)}">${SEG[k].map(o=>`<button type="button" role="radio" aria-checked="${REG.norm(v[k])===o}" class="${REG.norm(v[k])===o?'on':''}" data-ifseg="${k}" data-v="${esc(REG.enumLabel(o))}">${esc(REG.enumLabel(o))}</button>`).join('')}</div>`;
      else if(c.type==='enum')input=`<select id="if_${k}" data-if="${k}" class="${v[k]?'':'ph'}"><option value="" disabled ${v[k]?'':'selected'} hidden>Select ${esc((LABEL[k]||c.label).toLowerCase())}</option>${c.opts.filter(Boolean).map(o=>`<option ${REG.norm(REG.enumLabel(o))===REG.norm(v[k])?'selected':''}>${esc(REG.enumLabel(o))}</option>`).join('')}</select>`;
      else if(c.type==='ref')input=`<select id="if_${k}" data-if="${k}" class="${v[k]?'':'ph'}"><option value="" ${v[k]?'':'selected'}>Select role</option>${S.active(c.ref).map(x=>`<option ${REG.norm(x.name)===REG.norm(v[k])?'selected':''}>${esc(x.name)}</option>`).join('')}</select>`;
      else input=`<div class="inx">${`<input id="if_${k}" data-if="${k}" value="${esc(v[k])}" ${c.type==='num'?'inputmode="decimal"':''} ${unit?'class="hsfx"':''}>`}${unit?`<span class="sfx">${esc(unit)}</span>`:''}</div>`;
      return `<div class="fld ${full?'full':''}">${lbl(k,req)}${input}${errOf(k)}</div>`;};
    const body=()=>{
      const kind=f.root?REG.itemKindOfCat(f.root):'general';
      if(f.tab==='stock'&&rec)return stockPane();
      const subs=f.root?S.active('categories').filter(c=>c.id!==f.root&&subtree(f.root).has(c.id)).map(c=>REG.catPath(c.id).split(' > ').slice(1).join(' > ')):[];
      const req=new Set(REG.ITEM_REQ[kind]||[]);const subReq=!!REG.ITEM_SUB_REQ[kind];
      const card=(title,inner,extra)=>`<section class="isec">${title?`<div class="isec-h"><h4>${title}</h4>${extra||''}</div>`:''}<div class="isec-b">${inner}</div></section>`;
      const swSec=(sw,title,hint,inner)=>{const on=v[sw]==='yes';return `<section class="isec ${on?'on':'off'}"><label class="isec-h capsw"><span class="isec-t"><h4>${esc(title)}</h4>${hint?`<span class="ihint">${esc(hint)}</span>`:''}</span><input type="checkbox" role="switch" aria-label="${esc(title)}" data-ifcap="${sw}" ${on?'checked':''}><span class="knob"></span></label>${errOf(sw)}${on?`<div class="isec-b">${inner}</div>`:''}</section>`;};
      const uses=rec?REG.itemUses(rec.id):[];
      const kindLinks={medicine:['Health Config','#/health/config'],vaccine:['Vaccination plan','#/vaccination/plan'],feed:['Feed Config','#/feed/config']}[kind];
      const chips=uses.map(u=>`<a class="tag ${u.type==='sop'?'t-info':'t-mut'}" style="text-decoration:none" href="${u.href}" title="${esc(u.sub||'')}">${esc(u.label)}${u.type==='sop'&&!u.published?' · draft':''} ›</a>`);
      if(kindLinks&&!uses.some(u=>u.href===kindLinks[1]))chips.push(`<a class="tag t-mut" style="text-decoration:none" href="${kindLinks[1]}">${kindLinks[0]} ›</a>`);
      const listFld=(rec&&recRoot&&recRoot.system)||(!rec&&r0&&r0.system&&f.root===r0.id)
        ?`<div class="fld"><label>Catalogue</label><div class="ro">${ic('lock','',13)}${esc((recRoot||r0).name)}</div></div>`
        :`<div class="fld"><label for="if_root">Catalogue / list<i class="rq"></i></label><select id="if_root" data-ifr class="${f.root?'':'ph'}"><option value="" disabled hidden ${f.root?'':'selected'}>Select list</option>${roots.map(c=>`<option value="${c.id}" ${f.root===c.id?'selected':''}>${esc(c.name)}</option>`).join('')}</select>${f.root?'':shown?'<div class="ferr">Required</div>':''}</div>`;
      const subOpts=[...new Set(subs.concat(f.sub&&!f.newSub&&!subs.includes(f.sub)?[f.sub]:[]))];
      const subFld=`<div class="fld"><label for="if_sub">Subcategory${subReq?'<i class="rq"></i>':''}</label>${f.newSub
        ?`<div class="subnew"><input id="if_sub" data-ifs value="${esc(f.sub)}" placeholder="New subcategory" aria-label="New subcategory"><button class="btn icon gh" data-ifsubx aria-label="Cancel new subcategory">${ic('x')}</button></div>`
        :`<select id="if_sub" data-ifsel class="${f.sub?'':'ph'}" ${f.root?'':'disabled'}><option value="">${subReq?'Select subcategory':'None'}</option>${subOpts.map(x=>`<option ${x===f.sub?'selected':''}>${esc(x)}</option>`).join('')}<option value="__new">+ New subcategory</option></select>`}${errOf('category')}</div>`;
      const o=owner(),cur=deptList();
      const pills=(o?[o]:[]).concat(DEPT_OPTS().filter(d=>d&&d!==o)).map(d=>d===o
        ?`<span class="dpill on lock" title="${esc(REG.catRoot(f.root).name)} always show in ${esc(d)}"><input type="checkbox" data-ifd value="${esc(d)}" checked disabled aria-label="${esc(d)} (always on)">${ic('lock','',12)}${esc(d)}</span>`
        :`<label class="dpill ${cur.includes(d)?'on':''}"><input type="checkbox" data-ifd value="${esc(d)}" ${cur.includes(d)?'checked':''}>${cur.includes(d)?ic('check','',12):''}${esc(d)}</label>`).join('');
      const st=rec?REG.stockOf(rec.id):null;
      return `${card('Details',`<div class="fgrid">
          <div class="fld full">${lbl('name',1).replace('for="if_name"','for="if_name"')}<input id="if_name" data-if="name" value="${esc(v.name)}" placeholder="${kind==='vaccine'?'e.g. PPR vaccine':'Name'}">${errOf('name')}</div>
          ${listFld}${subFld}
          ${KIND_FIELDS[kind].map(k=>field(k,req.has(k))).join('')}
          ${rec?`<div class="fld"><label>Status</label><div class="seg iseg">${['active','archived'].map(x=>`<button type="button" class="${f.status===x?'on':''}" data-ifst="${x}">${REG.enumLabel(x)}</button>`).join('')}</div></div>`:''}
        </div>`)}
        ${card('Departments',`<div class="dpills">${pills}</div>`)}
        ${swSec('trackStock','Track stock',st&&v.trackStock==='yes'?st.qty+' in stock'+(st.lots?' · '+st.lots+' lot'+(st.lots===1?'':'s'):''):'',`${countUnit(v.unit)?`<label class="irow capsw"><span class="isec-t"><b>Tracked by serial numbers</b></span><input type="checkbox" role="switch" aria-label="Tracked by serial numbers" data-ifser ${v.serialized==='yes'?'checked':''}><span class="knob"></span></label>`:''}<div class="fgrid">${field('reorderAt',1)+field('stockRole',1)}</div>${S.active('parks').length>1?`<div class="rbp"><div class="rbp-h">Reorder at by park</div>${S.active('parks').map(p=>`<div class="rbp-r"><span>${esc(p.name)}</span><div class="inx"><input data-rbp="${p.id}" inputmode="decimal" value="${esc(rbp[p.id]==null?'':rbp[p.id])}" placeholder="${esc(v.reorderAt||'')}" aria-label="Reorder at, ${esc(p.name)}" ${v.unit?'class="hsfx"':''}>${v.unit?`<span class="sfx">${esc(v.unit)}</span>`:''}</div></div>`).join('')}</div>`:''}`)}
        ${swSec('trackExpiry','Track expiry',st&&v.trackExpiry==='yes'&&st.expiresOn?'Nearest expiry '+fmtD(st.expiresOn):'',`<div class="fgrid">${field('expiryAlertDays',1)+field('expiryRole',1)}</div>`)}
        ${kind==='general'?swSec('trackCheck','Recurring check','Schedule a check with proof',`<div class="fgrid">${field('checkEvery',1)+field('checkRole',1)+field('proof',0,1)}</div>`):''}
        ${rec?card('Used in',`<div class="chips" data-usedin>${chips.join('')||'<span class="muted">Not used yet</span>'}</div>`):''}
        ${shown&&res&&res.issues._?`<div class="ferr mt">${esc(res.issues._[1])}</div>`:''}`;
    };
    /* Stock tab: lots are transactions (receive / consume-adjust / transfer), never typed on the item */
    const stockPane=()=>{
      const lots=S.active('lots').filter(l=>l.itemId===rec.id&&+l.qty>0).sort((a,b)=>(a.expiresOn||'9999').localeCompare(b.expiresOn||'9999'));
      const st=REG.stockOf(rec.id),unit=v.unit||'';const a=f.act;
      const parks=S.active('parks');const popt=(sel,ex)=>parks.filter(p=>p.id!==ex).map(p=>`<option value="${p.id}" ${p.id===sel?'selected':''}>${esc(p.name)}</option>`).join('');
      const actForm=!a?'':a.kind==='receive'?`<section class="isec on"><div class="isec-h"><h4>Receive lot</h4></div><div class="isec-b"><div class="fgrid">
          <div class="fld"><label>Lot no.<i class="rq"></i></label><input data-sa="lotNo" value="${esc(a.lotNo||'')}"></div>
          <div class="fld"><label>Park<i class="rq"></i></label><select data-sa="parkId" class="${a.parkId?'':'ph'}"><option value="">Select park</option>${popt(a.parkId)}</select></div>
          ${v.serialized==='yes'&&countUnit(unit)?`<div class="fld"><label>Serial from<i class="rq"></i></label><input data-sa="serialFrom" value="${esc(a.serialFrom||'')}" placeholder="SF-048-00001"></div>
          <div class="fld"><label>Serial to<i class="rq"></i></label><input data-sa="serialTo" value="${esc(a.serialTo||'')}" placeholder="SF-048-00500"></div>`
          :`<div class="fld"><label>Quantity<i class="rq"></i></label><div class="inx"><input data-sa="qty" inputmode="decimal" value="${esc(a.qty||'')}" class="${unit?'hsfx':''}">${unit?`<span class="sfx">${esc(unit)}</span>`:''}</div></div>`}
          <div class="fld"><label>Vendor</label><select data-sa="vendorId" class="${a.vendorId?'':'ph'}"><option value="">Select vendor</option>${S.active('vendors').map(x=>`<option value="${x.id}" ${x.id===a.vendorId?'selected':''}>${esc(x.name)}</option>`).join('')}</select></div>
          <div class="fld"><label>Expires on</label><div class="inx date">${ic('calendar','',15)}<input type="date" data-sa="expiresOn" value="${esc(a.expiresOn||'')}"></div></div>
          <div class="fld"><label>Received on</label><div class="inx date">${ic('calendar','',15)}<input type="date" data-sa="receivedOn" value="${esc(a.receivedOn||new Date().toISOString().slice(0,10))}"></div></div>
        </div>${a.err?`<div class="ferr">${esc(a.err)}</div>`:''}<div class="iacts"><button class="btn sm" data-sx>Cancel</button><button class="btn sm p" data-sgo>Receive</button></div></div></section>`
        :`<section class="isec on"><div class="isec-h"><h4>${a.kind==='transfer'?'Transfer':'Use / adjust'} · lot ${esc((S.get('lots',a.lotId)||{}).lotNo||'')}</h4></div><div class="isec-b"><div class="fgrid">
          ${a.kind==='transfer'?`<div class="fld"><label>To park<i class="rq"></i></label><select data-sa="toParkId" class="${a.toParkId?'':'ph'}"><option value="">Select park</option>${popt(a.toParkId,(S.get('lots',a.lotId)||{}).parkId)}</select></div>
            <div class="fld"><label>Quantity<i class="rq"></i></label><div class="inx"><input data-sa="qty" inputmode="decimal" value="${esc(a.qty||'')}" class="${unit?'hsfx':''}">${unit?`<span class="sfx">${esc(unit)}</span>`:''}</div></div>`
          :`<div class="fld full"><label>Reason</label><div class="seg iseg">${['Used','Damaged','Count correction'].map(x=>`<button type="button" class="${(a.reason||'Used')===x?'on':''}" data-sreason="${x}">${x}</button>`).join('')}</div></div>
            <div class="fld"><label>Quantity out</label><div class="inx"><input data-sa="used" inputmode="decimal" value="${esc(a.used||'')}" placeholder="0" class="${unit?'hsfx':''}">${unit?`<span class="sfx">${esc(unit)}</span>`:''}</div></div>
            <div class="fld"><label>Or set count to</label><div class="inx"><input data-sa="count" inputmode="decimal" value="${esc(a.count||'')}" placeholder="${esc((S.get('lots',a.lotId)||{}).qty)}" class="${unit?'hsfx':''}">${unit?`<span class="sfx">${esc(unit)}</span>`:''}</div></div>`}
        </div>${a.err?`<div class="ferr">${esc(a.err)}</div>`:''}<div class="iacts"><button class="btn sm" data-sx>Cancel</button><button class="btn sm p" data-sgo>${a.kind==='transfer'?'Transfer':'Save'}</button></div></div></section>`;
      const policy=[v.trackStock==='yes'?UI.tag('Reorder at '+(REG.reorderLevel(rec)==null?'—':REG.reorderLevel(rec)),REG.lowStock(rec)?'warn':'mut'):'',v.trackExpiry==='yes'&&st.expiresOn&&(Date.parse(st.expiresOn)-Date.now())/864e5<=(+v.expiryAlertDays||0)?UI.tag('Expiry alert','warn'):''].join(' ');
      return `<section class="isec"><div class="isec-h"><h4>${st.qty} ${esc(unit)} in stock</h4>${policy}<span class="sp"></span><button class="btn sm p" data-sact="receive">${ic('plus')}Receive</button>${st.expiresOn?`<span class="ihint" style="flex-basis:100%;margin-top:-6px">Nearest expiry ${fmtD(st.expiresOn)}</span>`:''}</div>
        ${lots.length?`<div class="isec-b flush"><table class="lots"><thead><tr><th>Lot</th><th>Park</th><th class="num">Qty</th><th>Expires</th><th>Received</th><th></th></tr></thead><tbody>${lots.map(l=>`<tr><td data-label="Lot"><b>${esc(l.lotNo)}</b>${l.serialFrom?`<div class="muted small mono">${esc(l.serialFrom)} – ${esc(l.serialTo)}</div>`:''}${l.vendorId?`<div class="muted small">${esc(REG.labelById('vendors',l.vendorId))}</div>`:''}</td><td data-label="Park">${esc(REG.labelById('parks',l.parkId))}</td><td class="num" data-label="Qty">${esc(l.qty)}</td><td data-label="Expires">${esc(fmtD(l.expiresOn)||'—')}</td><td data-label="Received">${esc(fmtD(l.receivedOn))}</td>
          <td class="act"><button class="btn icon gh" data-slmenu="${l.id}" aria-label="Lot actions">${ic('more')}</button></td></tr>`).join('')}</tbody></table></div>`
          :`<div class="isec-b"><div class="empty">No stock yet</div></div>`}</section>${actForm}`;
    };
    const rowOf=()=>{const kind=f.root?REG.itemKindOfCat(f.root):'general';const out=Object.assign({},v);
      const root=S.get('categories',f.root);out.category=root?root.name+(f.sub.trim()?' > '+f.sub.trim().split('>').map(x=>x.trim()).filter(Boolean).join(' > '):''):'';
      Object.keys(KIND_FIELDS).forEach(kk=>KIND_FIELDS[kk].forEach(k=>{if(!KIND_FIELDS[kind].includes(k))out[k]='';}));
      Object.entries(REG.ITEM_CAPS).forEach(([sw,c])=>{if(c.general&&kind!=='general')out[sw]='no';if(out[sw]!=='yes')c.fields.forEach(k=>out[k]='');});
      if(!countUnit(out.unit))out.serialized='no';
      if(root)out.depts=REG.withOwner(String(out.depts).split(/;\s*/).filter(Boolean),root.id).join('; ');
      if(rec)out.status=f.status;return out;};
    const save=again=>{
      shown=true;const row={id:'f',v:rowOf(),recId:rec?rec.id:null};
      res=REG.validate('items',[row],{createParents:true,strict:true})[0];
      const dup=REG.norm(row.v.name)&&S.all('items').some(x=>x.id!==(rec&&rec.id)&&REG.norm(x.name)===REG.norm(row.v.name));
      if(dup){res.issues.name=['err','Already exists'];res.status='error';}
      if(!f.root){res.status='error';}
      if(f.newSub&&!f.sub.trim()){res.issues.category=['err','Required'];res.status='error';}
      if(rec&&f.status==='archived'&&rec.status!=='archived'){const b=REG.archiveBlock('items',rec.id);if(b){res.issues.name=['err','Cannot archive · '+b];res.status='error';}}
      if(v.trackStock==='yes'){const bad=Object.entries(rbp).find(([,x])=>isNaN(+x)||+x<0);if(bad){res.issues.reorderAt=['err','Park level: 0 or more'];res.status='error';}}
      if(res.status==='error'){UI.redrawDrawer({body:body()});const e=document.querySelector('#drawer .ferr');if(e)e.scrollIntoView({block:'center'});return;}
      const n=S.snap(rec?'Saved item':'Added item');REG.commit('items',[row],{createParents:true});
      {const saved=rec||S.active('items').find(x=>REG.norm(x.name)===REG.norm(row.v.name));if(saved){if(v.trackStock==='yes'){const o={};Object.entries(rbp).forEach(([k,x])=>{if(String(x).trim()!=='')o[k]=+x;});saved.reorderByPark=o;}else delete saved.reorderByPark;}}
      S.save();
      UI.closeDrawer();App.render();UI.toast((rec?'Saved ':'Added ')+row.v.name,()=>{S.undoTo(n);App.render();});
      if(again)itemForm(null,{cat:f.sub.trim()?(REG.findPath(row.v.category)||f.root):f.root});
    };
    const kind0=()=>f.root?REG.itemKindOfCat(f.root):'general';
    const head=()=>{const kind=kind0();const rootC=S.get('categories',f.root);
      const crumb=(rootC?rootC.name:'Items')+' · '+(rec?(f.sub?f.sub.split(' > ').join(' › '):'Edit '+KIND_ONE[kind]):'New '+KIND_ONE[kind]);
      return `<div class="ihead"><span class="ikic k-${kind}">${ic(KIND_ICON[kind],'',18)}</span><div class="ihead-t"><div class="icrumb">${esc(crumb)}</div><h2>${esc(rec?rec.name:(v.name||'New '+KIND_ONE[kind]))}</h2></div></div>
        ${rec?`<div class="itabs seg"><button class="${f.tab==='details'?'on':''}" data-iftab="details">Details</button><button class="${f.tab==='stock'?'on':''}" data-iftab="stock">Stock <span class="muted">${REG.stockOf(rec.id).qty}</span></button></div>`:''}`;};
    const foot=()=>f.tab==='stock'?`<button class="btn" data-a="drawer-close">Close</button><span class="sp"></span>`
      :`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span>${rec?'':`<button class="btn" data-ifsave="again">Save and add another</button>`}<button class="btn p" data-ifsave>Save</button>`;
    const redraw=()=>UI.redrawDrawer({head:head(),body:body(),foot:foot()});
    const stockGo=()=>{const a=f.act;const num=x=>x!==''&&x!=null&&!isNaN(+x)&&+x>=0;
      if(a.kind==='receive'&&v.serialized==='yes'&&countUnit(v.unit)){const c=REG.serialCount(a.serialFrom,a.serialTo);if(!c){a.err='Serial range must share a prefix and end in increasing numbers';return redraw();}
        const hit=REG.serialOverlap(rec.id,a.serialFrom,a.serialTo);if(hit){a.err='Serial range overlaps lot '+hit.lotNo;return redraw();}a.qty=c;}
      if(a.kind==='receive'&&S.all('lots').some(l=>l.itemId===rec.id&&REG.norm(l.lotNo)===REG.norm(a.lotNo))){a.err='Lot already received';return redraw();}
      if(a.kind==='receive'){if(!String(a.lotNo||'').trim()||!a.parkId||!num(a.qty)||!+a.qty){a.err='Lot no., park and quantity are required';return redraw();}
        const n=S.snap('Received lot');REG.receive({itemId:rec.id,lotNo:a.lotNo.trim(),parkId:a.parkId,qty:a.qty,serialFrom:a.serialFrom,serialTo:a.serialTo,vendorId:a.vendorId,expiresOn:a.expiresOn,receivedOn:a.receivedOn});S.save();f.act=null;redraw();App.render();UI.toast('Received lot '+a.lotNo,()=>{S.undoTo(n);App.render();});return;}
      const lot=S.get('lots',a.lotId);if(!lot)return;
      if(a.kind==='transfer'){if(!a.toParkId||!num(a.qty)||!+a.qty||+a.qty>+lot.qty){a.err='Choose a park and a quantity up to '+lot.qty;return redraw();}
        const n=S.snap('Transfer');REG.transfer(lot.id,a.toParkId,a.qty);S.save();f.act=null;redraw();App.render();UI.toast('Transferred '+a.qty,()=>{S.undoTo(n);App.render();});return;}
      let to=null;if(num(a.count)&&String(a.count).trim()!=='')to=+a.count;else if(num(a.used)&&+a.used>0&&+a.used<=+lot.qty)to=+lot.qty-(+a.used);
      if(to==null){a.err='Enter used (up to '+lot.qty+') or a new count';return redraw();}
      const mx=REG.serialMax(lot);if(mx!=null&&to>mx){a.err='Count can be at most '+mx+' (serial range)';return redraw();}
      const n=S.snap('Adjust');REG.adjust(lot.id,to,a.reason||'Used');S.save();f.act=null;redraw();App.render();UI.toast('Lot '+lot.lotNo+' now '+to,()=>{S.undoTo(n);App.render();});};
    const snapF=()=>JSON.stringify([v,f.root,f.sub,f.newSub,f.status,rbp]);const init0=snapF();
    const actTyped=()=>!!(f.act&&['lotNo','parkId','qty','serialFrom','serialTo','vendorId','expiresOn','used','count','toParkId'].some(k=>String(f.act[k]==null?'':f.act[k]).trim()));
    UI.drawer({head:head(),body:body(),wide:true,cls:'idr',foot:foot(),dirty:()=>snapF()!==init0||actTyped(),
      onClose(){const dr=document.getElementById('drawer');if(dr&&dr._ifH){['input','click','change'].forEach(k=>dr.removeEventListener(k,dr._ifH[k]));dr._ifH=null;}},
      mount(dr){
        if(dr._ifH)['input','click','change'].forEach(k=>dr.removeEventListener(k,dr._ifH[k]));
        const H=dr._ifH={};
        dr.addEventListener('input',H.input=e=>{const t=e.target;if(t.matches('[data-if]')){v[t.dataset.if]=t.value;if(t.dataset.if==='name'&&!rec){const h=dr.querySelector('.ihead h2');if(h)h.textContent=t.value||'New '+KIND_ONE[kind0()];}}if(t.matches('[data-ifs]'))f.sub=t.value;if(t.matches('[data-rbp]')){const x=t.value.trim();if(x==='')delete rbp[t.dataset.rbp];else rbp[t.dataset.rbp]=x;}if(t.matches('[data-sa]')&&f.act)f.act[t.dataset.sa]=t.value;});
        dr.addEventListener('click',H.click=e=>{const t=e.target;
          if(t.closest('[data-ifsubx]')){f.newSub=false;f.sub='';redraw();}
          const sg=t.closest('[data-ifseg]');if(sg){v[sg.dataset.ifseg]=sg.dataset.v;redraw();}
          const s2=t.closest('[data-ifst]');if(s2){f.status=s2.dataset.ifst;redraw();}
          const tb=t.closest('[data-iftab]');if(tb){f.tab=tb.dataset.iftab;f.act=null;redraw();}
          const sv=t.closest('[data-ifsave]');if(sv)save(sv.dataset.ifsave==='again');
          const sa=t.closest('[data-sact]');if(sa){f.act={kind:sa.dataset.sact,lotId:sa.dataset.lot||''};redraw();const i=dr.querySelector('[data-sa]');if(i)i.focus();}
          if(t.closest('[data-sx]')){f.act=null;redraw();}
          if(t.closest('[data-sgo]'))stockGo();
          const rs=t.closest('[data-sreason]');if(rs&&f.act){f.act.reason=rs.dataset.sreason;redraw();}
          const lm=t.closest('[data-slmenu]');if(lm){e.stopPropagation();const l=S.get('lots',lm.dataset.slmenu);const exp=l.expiresOn&&Date.parse(l.expiresOn)<Date.now();
            UI.menu(lm,[{label:'Use / adjust',icon:'edit-3',run:()=>{f.act={kind:'adjust',lotId:l.id};redraw();}},{label:'Transfer',icon:'truck',run:()=>{f.act={kind:'transfer',lotId:l.id};redraw();}}].concat(exp?['-',{label:'Write off',icon:'trash',danger:true,run:()=>{const n=S.snap('Write off');REG.writeOff(l.id);S.save();redraw();App.render();UI.toast('Wrote off lot '+l.lotNo,()=>{S.undoTo(n);App.render();});}}]:[]));}
          const wo=t.closest('[data-swo]');if(wo){const l=S.get('lots',wo.dataset.swo);const n=S.snap('Write off');REG.writeOff(l.id);S.save();redraw();App.render();UI.toast('Wrote off lot '+l.lotNo,()=>{S.undoTo(n);App.render();});}});
        dr.addEventListener('change',H.change=e=>{const t=e.target;
          if(t.matches('[data-if]')){v[t.dataset.if]=t.value;t.classList.toggle('ph',!t.value);}
          if(t.matches('[data-sa]')&&f.act){f.act[t.dataset.sa]=t.value;if(t.tagName==='SELECT')t.classList.toggle('ph',!t.value);}
          if(t.matches('[data-ifser]')){v.serialized=t.checked?'yes':'no';redraw();}
          if(t.matches('[data-ifcap]')){const sw=t.dataset.ifcap;v[sw]=t.checked?'yes':'no';if(!t.checked)REG.ITEM_CAPS[sw].fields.forEach(k=>v[k]='');redraw();}
          if(t.matches('[data-ifsel]')){if(t.value==='__new'){f.newSub=true;f.sub='';redraw();const i=dr.querySelector('[data-ifs]');if(i)i.focus();}else{f.sub=t.value;redraw();}}
          if(t.matches('[data-ifd]')){const o=owner();if(t.value===o){t.checked=true;return;}setDepts([...dr.querySelectorAll('[data-ifd]:checked')].map(x=>x.value));redraw();}
          if(t.matches('[data-ifr]')){const oldO=owner();f.root=t.value;f.sub='';f.newSub=false;const r=S.get('categories',f.root);
            let cur=deptList().filter(x=>x!==oldO);if(!rec&&r&&!cur.length)cur=r.depts||[];setDepts(cur);redraw();}});
      }});
  }

  Object.assign(A,{
    'list-new'(){newListDrawer();},
    'cat-arch'(el){catArch=!!el.dataset.v;catSel='';App.render();},
    'cat-sel'(el,e){if(e.target.closest('[data-a="cat-menu"]'))return;catSel=el.dataset.id;App.render();},
    'cat-add'(){const i=document.querySelector('[data-newcat]');const v=i&&i.value.trim();if(!v)return;
      if(S.active('categories').some(c=>(c.parentId||'')===catSel&&REG.norm(c.name)===REG.norm(v))){UI.toast('“'+v+'” already exists');return;}
      const n=S.snap('Added subcategory');S.add('categories',{name:v,parentId:catSel});S.save();App.render();UI.toast('Added '+v,()=>{S.undoTo(n);App.render();});},
    'cat-menu'(el,e){e.stopPropagation();const c=S.get('categories',el.dataset.id);const fixedRoot=c.system&&isRoot(c);
      const kind=REG.itemKindOfCat(c.id),root=REG.catRoot(c.id);
      const parents=fixedRoot?[]:S.active('categories').filter(x=>!subtree(c.id).has(x.id)&&REG.itemKindOfCat(x.id)===kind&&(kind==='general'||subtree(root.id).has(x.id)));
      UI.menu(el,[
      {label:'Add subcategory',icon:'plus',run(){catSel=c.id;catArch=false;App.render();setTimeout(()=>{const i=document.querySelector('[data-newcat]');if(i)i.focus();},0);}},
      {label:fixedRoot?'Rename':'Rename / move',icon:'edit-3',run(){UI.drawer({title:fixedRoot?'Catalogue':'List',body:`<div class="fld"><label>Name</label><input data-cren value="${esc(c.name)}"></div>${fixedRoot?'':`<div class="fld"><label>Parent</label><select data-cpar>${kind==='general'?'<option value="">Top level</option>':''}${parents.map(x=>`<option value="${x.id}" ${x.id===c.parentId?'selected':''}>${esc(REG.catPath(x.id))}</option>`).join('')}</select></div>`}
          ${isRoot(c)&&!fixedRoot?`<div class="fld"><label>Departments</label><div class="chkrow">${DEPT_OPTS().map(d=>`<label><input type="checkbox" data-cdep value="${esc(d)}" ${(c.depts||[]).includes(d)?'checked':''}>${esc(d)}</label>`).join('')}</div></div>`:''}`,
        foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-crsave>Save</button>`,
        mount(dr){dr.querySelector('[data-crsave]').onclick=()=>{const nm=dr.querySelector('[data-cren]').value.trim(),ps=dr.querySelector('[data-cpar]'),par=ps?ps.value:(c.parentId||'');if(!nm)return;
          const deps=dr.querySelector('[data-cdep]')?[...dr.querySelectorAll('[data-cdep]:checked')].map(x=>x.value):null;
          if(S.active('categories').some(x=>x.id!==c.id&&(x.parentId||'')===par&&REG.norm(x.name)===REG.norm(nm))){UI.toast('“'+nm+'” already exists there');return;}
          UI.closeDrawer();UI.undoable('Saved',()=>{c.name=nm;if(!fixedRoot)c.parentId=par;if(deps)c.depts=deps;App.render();});};}});}},
      ...(fixedRoot?[]:[c.status==='archived'?{label:'Restore',icon:'rotate',run(){UI.undoable('Restored',()=>{subtree(c.id).forEach(id=>{const x=S.get('categories',id);if(x)x.status='active';});let p=S.get('categories',c.parentId);while(p){p.status='active';p=S.get('categories',p.parentId);}App.render();});}}
        :{label:'Archive',icon:'archive',run(){if(catBlocked(c))return;const dr=REG.itemUses(null,c.id,true).filter(x=>x.type==='sop'&&!x.published);UI.undoable('Archived'+(dr.length?' · used in draft of '+dr[0].label+(dr.length>1?' +'+(dr.length-1):''):''),()=>{subtree(c.id).forEach(id=>{const x=S.get('categories',id);if(x)x.status='archived';});if(subtree(c.id).has(catSel))catSel='';App.render();});}},
      '-',{label:'Delete',icon:'trash',danger:true,run(){const ids=subtree(c.id);const n=S.all('items').filter(i=>ids.has(i.categoryId)).length;
        if(n){UI.toast('In use by '+n+' items · archive instead');return;}
        if(REG.itemUses(null,c.id).some(u=>u.type==='sop')){catBlocked(c);return;}
        UI.undoable('Deleted',()=>{S.remove('categories',[...ids]);catSel='';App.render();});}}])]);}
  });
  document.addEventListener('keydown',e=>{if(e.target.matches&&e.target.matches('[data-newcat]')&&e.key==='Enter')A['cat-add']();});

  /* ---------- park page: read-only pens table; edits happen in drawers or the grid ---------- */
  let PD=null; /* kept for the unsaved guard; the park page itself has no draft */
  const PARK={open:new Set()};
  const stageByName=name=>S.active('stages').filter(x=>REG.norm(x.name)===REG.norm(name)||REG.norm(x.code)===REG.norm(name));
  const farmIsNew=t=>{t=String(t||'').trim();return !!t&&!S.active('farms').some(f=>REG.norm(f.name)===REG.norm(t)||REG.norm(f.code)===REG.norm(t));};
  const partName=(pen,j)=>{const L=j<26?String.fromCharCode(65+j):'P'+(j+1);const b=String(pen||'').trim();return b?b+' '+L:L;};
  const dash='<span class="muted">—</span>';
  const human=x=>x?String(x)[0].toUpperCase()+String(x).slice(1):'';
  const pensOf=pk=>S.active('pens').filter(p=>p.parkId===pk).sort((a,b)=>REG.label('pens',a).localeCompare(REG.label('pens',b),undefined,{numeric:true}));
  function parkPage(id){
    const park=id!=='new'?S.get('parks',id):null;
    if(!park){setTimeout(()=>parkDrawer(null),0);
      return `<div class="phead"><div><div class="crumb">Configuration / <a href="#/configuration/items/parks">Items and settings</a> / <b>New park</b></div><h1>New park</h1></div></div><section class="card"><div class="empty">Name the park to start adding pens<div class="mt"><button class="btn p" data-a="pk-edit" data-id="">${ic('plus')}New park</button></div></div></section>`;}
    const pens=pensOf(park.id);const parts=S.active('partitions').filter(t=>pens.some(p=>p.id===t.penId));
    const animals=S.active('animals').filter(a=>a.parkId===park.id);
    const meta=[park.code,pens.length+' pen'+(pens.length===1?'':'s'),parts.length+' partition'+(parts.length===1?'':'s'),animals.length+' animal'+(animals.length===1?'':'s')].filter(Boolean).join(' · ');
    const icu=p=>p.hasIcu===true||p.hasIcu==='yes';const anyIcu=pens.some(icu);
    const row=p=>{const ps=parts.filter(t=>t.penId===p.id),n=animals.filter(a=>a.penId===p.id).length,open=PARK.open.has(p.id);
      return `<tr class="clk pkrow ${open?'open':''}" data-a="pk-toggle" data-id="${p.id}" aria-expanded="${open}">
        <td data-label="Pen"><span class="pkname"><span class="chev">${ic('chevron-right','',14)}</span><b>${esc(p.name)}</b></span><div class="pksum">${esc([ps.length?ps.length+' partition'+(ps.length===1?'':'s'):'',p.capacity!==''&&p.capacity!=null?'cap '+p.capacity:'',human(p.sex||'mixed'),p.stageId?REG.labelById('stages',p.stageId):'',p.hasIcu===true||p.hasIcu==='yes'?'ICU':'',n?n+' animal'+(n===1?'':'s'):''].filter(Boolean).join(' · '))}</div></td>
        <td class="num" data-label="Partitions">${ps.length||dash}</td>
        <td class="num" data-label="Capacity">${p.capacity===''||p.capacity==null?dash:esc(p.capacity)}</td>
        <td data-label="Sex">${esc(human(p.sex||'mixed'))}</td>
        <td data-label="Stage">${p.stageId?esc(REG.labelById('stages',p.stageId)):dash}</td>
        ${anyIcu?`<td data-label="ICU">${icu(p)?UI.tag('ICU','info'):dash}</td>`:''}
        <td class="num" data-label="Animals">${n||dash}</td>
        <td class="act"><button class="btn icon gh" data-a="pk-penmenu" data-id="${p.id}" aria-label="Pen actions">${ic('more')}</button></td></tr>
        ${open?`<tr class="pkdet"><td colspan="${anyIcu?8:7}"><div class="pkparts">${ps.map(t=>`<button class="pchip" data-a="pk-editpen" data-id="${p.id}">${esc(t.name)}${t.capacity!==''&&t.capacity!=null?` <span class="muted">· cap ${esc(t.capacity)}</span>`:''}</button>`).join('')}
          <button class="btn sm gh" data-a="pk-addpart" data-id="${p.id}">${ic('plus','',13)}Add partition</button></div></td></tr>`:''}`;};
    return `<div class="phead"><div><div class="crumb">Configuration / <a href="#/configuration/items/parks">Items and settings</a> / <b>${esc(park.name)}</b></div><h1>${esc(park.name)}</h1><div class="pkmeta">${esc(meta)}${park.farmId&&REG.norm(REG.labelById('farms',park.farmId))!==REG.norm(park.name)?' · '+esc(REG.labelById('farms',park.farmId)):''}</div></div><div class="sp"></div>
      <button class="btn" data-a="pk-edit" data-id="${park.id}">${ic('edit-3')}Edit park</button><button class="btn p" data-a="pk-add" data-id="${park.id}">${ic('plus')}Add pens${ic('chevron-down','',14)}</button></div>
      <section class="card"><div class="hd lhd"><div class="ttl"><h3>Pens <span class="cnt">${pens.length}</span></h3></div><span class="sp"></span>
        ${pens.length?`<button class="btn sm" data-a="pk-grid" data-id="${park.id}">${ic('sheet')}Edit in grid</button>`:''}</div>
        <div class="twrap screen ltable">${pens.length?`<table class="ltbl pktbl"><colgroup><col><col style="width:120px"><col style="width:100px"><col style="width:100px"><col style="width:160px">${anyIcu?'<col style="width:80px">':''}<col style="width:90px"><col style="width:56px"></colgroup>
          <thead><tr><th>Pen</th><th class="num">Partitions</th><th class="num">Capacity</th><th>Sex</th><th>Stage</th>${anyIcu?'<th>ICU</th>':''}<th class="num">Animals</th><th></th></tr></thead><tbody>${pens.map(row).join('')}</tbody></table>`
          :`<div class="empty">No pens yet<div class="mt"><button class="btn p" data-a="pk-add" data-id="${park.id}">${ic('plus')}Add pens</button></div></div>`}</div></section>`;
  }
  function parkMount(){}
  const drawerHead=(icon,crumb,title)=>`<div class="ihead"><span class="ikic">${ic(icon,'',18)}</span><div class="ihead-t"><div class="icrumb">${esc(crumb)}</div><h2>${esc(title)}</h2></div></div>`;
  const fldH=(label,inner,req,full,err)=>`<div class="fld ${full?'full':''}"><label>${esc(label)}${req?'<i class="rq"></i>':''}</label>${inner}${err?`<div class="ferr">${esc(err)}</div>`:''}</div>`;
  function parkDrawer(id){
    const park=id?S.get('parks',id):null;const f={farm:park?REG.labelById('farms',park.farmId):'',name:park?park.name:'',code:park?park.code||'':''};let err={};
    const body=()=>`<section class="isec"><div class="isec-b" style="border:0"><div class="fgrid">
      ${fldH('Park name',`<input data-pk="name" value="${esc(f.name)}">`,1,1,err.name)}
      ${fldH('Farm',`<input data-pk="farm" value="${esc(f.farm)}" list="pkfarms" placeholder="Search or type a new farm"><datalist id="pkfarms">${S.active('farms').map(x=>`<option value="${esc(x.name)}">`).join('')}</datalist>`,0,0,farmIsNew(f.farm)?'':'')}
      ${fldH('Code',`<input data-pk="code" value="${esc(f.code)}">`)}</div></div></section>`;
    UI.drawer({head:drawerHead('map-pin',park?park.name+' · Edit park':'Parks · New park',park?'Edit park':'New park'),cls:'idr',body:body(),
      foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-pksave>${park?'Save':'Create park'}</button>`,
      mount(dr){dr.querySelectorAll('[data-pk]').forEach(i=>i.oninput=()=>{f[i.dataset.pk]=i.value;});
        dr.querySelector('[data-pksave]').onclick=()=>{err={};const nm=f.name.trim();if(!nm)err.name='Required';
          const col=REG.R.parks.cols.find(c=>c.k==='farm');const fr=f.farm.trim()?REG.findRef('parks',col,f.farm,{}):{rec:null};
          if(nm&&S.all('parks').some(x=>(!park||x.id!==park.id)&&(x.farmId||'')===(fr.rec?fr.rec.id:'')&&REG.norm(x.name)===REG.norm(nm)))err.name='Already exists';
          if(Object.keys(err).length){UI.redrawDrawer({body:body()});return;}
          const n=S.snap('Park');let farmId=fr.rec?fr.rec.id:'';if(f.farm.trim()&&!fr.rec){REG.createRef('parks',col,f.farm.trim(),{});farmId=(REG.findRef('parks',col,f.farm,{}).rec||{}).id||'';}
          let p=park;if(p)Object.assign(p,{name:nm,code:f.code.trim(),farmId});else p=S.add('parks',{name:nm,code:f.code.trim(),farmId});S.save();UI.closeDrawer();
          if(!park){App._allow=true;location.hash='#/configuration/items/park/'+p.id;}else App.render();
          UI.toast((park?'Saved ':'Created ')+nm,()=>{S.undoTo(n);App.render();});};}});
  }
  function penDrawer(penId,opt){
    opt=opt||{};const pen=S.get('pens',penId);if(!pen)return;const park=S.get('parks',pen.parkId);
    /* stages are picked by id; a name shared across species carries its species */
    const stAll=S.active('stages');const dupN=n=>stAll.filter(x=>REG.norm(x.name)===REG.norm(n)).length>1;
    const stageOpts=stAll.map(x=>[x.id,x.name+(dupN(x.name)?' · '+(REG.labelById('species',x.speciesId)||'Any species'):'')]).sort((a,b)=>a[1].localeCompare(b[1]));
    const inPen=S.active('animals').filter(a=>a.penId===pen.id);
    const f={name:pen.name,capacity:pen.capacity==null?'':String(pen.capacity),stage:pen.stageId||'',sex:pen.sex||'mixed',icu:pen.hasIcu===true||pen.hasIcu==='yes',life:pen.lifecycleStatus||'',
      parts:S.active('partitions').filter(t=>t.penId===pen.id).map(t=>({id:t.id,name:t.name,capacity:t.capacity==null?'':String(t.capacity)}))};
    if(opt.addPart)f.parts.push({name:partName(f.name,f.parts.length),capacity:''});
    let err={};
    const sel=(k,opts,ph)=>`<select data-pn="${k}" class="${f[k]?'':'ph'}"><option value="">${ph}</option>${opts.map(([id,l])=>`<option value="${id}" ${id===f[k]?'selected':''}>${esc(l)}</option>`).join('')}</select>`;
    const init0=JSON.stringify(f);
    const body=()=>`<section class="isec"><div class="isec-h"><h4>Pen</h4></div><div class="isec-b"><div class="fgrid">
        ${fldH('Pen name',`<input data-pn="name" value="${esc(f.name)}">`,1,0,err.name)}
        ${fldH('Capacity',`<div class="inx"><input data-pn="capacity" inputmode="numeric" value="${esc(f.capacity)}" class="hsfx"><span class="sfx">animals</span></div>${!err.capacity&&f.capacity!==''&&!isNaN(+f.capacity)&&+f.capacity<inPen.length?`<div class="fwarn">Below ${inPen.length} animals in this pen</div>`:''}`,0,0,err.capacity)}
        ${fldH('Stage',sel('stage',stageOpts,'Any stage'))}
        ${fldH('Lifecycle status',sel('life',stageOpts,'None'))}
        ${fldH('Sex',`<div class="seg iseg">${['mixed','female','male'].map(x=>`<button type="button" class="${f.sex===x?'on':''}" data-pnsex="${x}">${human(x)}</button>`).join('')}</div>`)}
        <div class="fld"><label>ICU</label><label class="capsw pkicu"><input type="checkbox" role="switch" data-pnicu ${f.icu?'checked':''}><span class="knob"></span><span>${f.icu?'Has ICU':'No ICU'}</span></label></div>
      </div></div></section>
      <section class="isec"><div class="isec-h"><h4>Partitions</h4><span class="cnt">${f.parts.length}</span><span class="sp"></span><button class="btn sm" data-pnadd>${ic('plus','',13)}Add partition</button></div>
        ${f.parts.length?`<div class="isec-b"><div class="partlist">${f.parts.map((t,j)=>`<div class="partrow"><input data-pp="${j}" data-k="name" value="${esc(t.name)}" aria-label="Partition name"><div class="inx"><input data-pp="${j}" data-k="capacity" value="${esc(t.capacity)}" inputmode="numeric" placeholder="Capacity" aria-label="Partition capacity"></div><button class="btn icon gh" data-pndel="${j}" aria-label="Remove partition">${ic('x','',15)}</button></div>${err.del&&err.del[j]?`<div class="ferr partferr">In use by <a href="#/configuration/items/animals" data-pnusers="${t.id}">${err.del[j]} animal${err.del[j]===1?'':'s'}</a></div>`:''}`).join('')}</div>${err.parts?`<div class="ferr">${esc(err.parts)}</div>`:''}</div>`:''}</section>`;
    const redraw=()=>UI.redrawDrawer({body:body()});
    UI.drawer({head:drawerHead('layers',(park?park.name:'Park')+' · Edit pen',pen.name),cls:'idr',body:body(),dirty:()=>JSON.stringify(f)!==init0,
      foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-pnsave>Save</button>`,
      mount(dr){
        dr.oninput=e=>{const t=e.target;if(t.dataset.pn)f[t.dataset.pn]=t.value;if(t.dataset.pp!=null)f.parts[+t.dataset.pp][t.dataset.k]=t.value;};
        dr.onchange=e=>{const t=e.target;if(t.matches('select[data-pn]')){f[t.dataset.pn]=t.value;t.classList.toggle('ph',!t.value);}if(t.matches('[data-pnicu]')){f.icu=t.checked;redraw();}};
        dr.onclick=e=>{const t=e.target;const sx=t.closest('[data-pnsex]');if(sx){f.sex=sx.dataset.pnsex;redraw();}
          if(t.closest('[data-pnadd]')){f.parts.push({name:partName(f.name,f.parts.length),capacity:''});redraw();const ins=dr.querySelectorAll('[data-k="name"]');const l=ins[ins.length-1];if(l){l.focus();l.select();}}
          const dl=t.closest('[data-pndel]');if(dl){const j=+dl.dataset.pndel,pt=f.parts[j];const n=pt&&pt.id?S.active('animals').filter(a=>a.partitionId===pt.id).length:0;
            if(n){err.del={};err.del[j]=n;redraw();return;}f.parts.splice(j,1);err.del=null;redraw();}
          const pu=t.closest('[data-pnusers]');if(pu){e.preventDefault();const ids=S.active('animals').filter(a=>a.partitionId===pu.dataset.pnusers).map(a=>a.id);const pt=S.get('partitions',pu.dataset.pnusers);UI.closeDrawer();List.showIds('animals',ids,'Using '+(pt?REG.fullLabel('partitions',pt):'partition'),'partitions');}
          if(t.closest('[data-pnsave]')){err={};const nm=f.name.trim();if(!nm)err.name='Required';
            else if(S.active('pens').some(x=>x.id!==pen.id&&x.parkId===pen.parkId&&REG.norm(x.name)===REG.norm(nm)))err.name='Already exists';
            if(f.capacity!==''&&(isNaN(+f.capacity)||+f.capacity<0))err.capacity='0 or more';
            const seen={};f.parts.forEach(t=>{const n=REG.norm(t.name);if(!n)err.parts='Partition name required';else if(seen[n])err.parts='Duplicate partition '+t.name;seen[n]=1;if(t.capacity!==''&&(isNaN(+t.capacity)||+t.capacity<0))err.parts='Partition capacity: 0 or more';});
            if(Object.keys(err).length){redraw();return;}
            const gone=S.active('partitions').filter(t=>t.penId===pen.id&&!f.parts.some(x=>x.id===t.id)).map(t=>[t,S.active('animals').filter(a=>a.partitionId===t.id).length]).filter(x=>x[1]);
            if(gone.length){err.parts=gone.map(([t,n])=>t.name+' in use by '+n+' animal'+(n===1?'':'s')).join(' · ');redraw();return;}
            const n=S.snap('Pen');const st=f.stage?S.get('stages',f.stage):null,lf=f.life?S.get('stages',f.life):null;
            const was=pen.name;Object.assign(pen,{name:nm,capacity:f.capacity===''?'':+f.capacity,stageId:st?st.id:'',stageName:st?st.name:'',sex:f.sex,hasIcu:f.icu,lifecycleStatus:lf?lf.id:''});
            if(!pen.displayName||pen.displayName===was)pen.displayName=nm;
            const keep=new Set();f.parts.forEach(t=>{const a={penId:pen.id,name:t.name.trim(),capacity:t.capacity===''?'':+t.capacity};let r=t.id?S.get('partitions',t.id):null;if(r)Object.assign(r,a,{status:'active'});else r=S.add('partitions',a);keep.add(r.id);});
            S.active('partitions').filter(t=>t.penId===pen.id&&!keep.has(t.id)).forEach(t=>t.status='archived');
            S.save();PARK.open.add(pen.id);UI.closeDrawer();App.render();UI.toast('Saved '+nm,()=>{S.undoTo(n);App.render();});}};}});
  }
  function generateDrawer(parkId){
    const park=S.get('parks',parkId);const g={n:'4',cap:'40',prefix:'Pen',start:String(pensOf(parkId).length+1),parts:'0'};
    const num=(k,l,sfx)=>fldH(l,`<div class="inx"><input data-g="${k}" inputmode="numeric" value="${esc(g[k])}" ${sfx?'class="hsfx"':''}>${sfx?`<span class="sfx">${sfx}</span>`:''}</div>`);
    const prev=()=>{const n=Math.min(200,Math.max(0,parseInt(g.n)||0)),s=parseInt(g.start)||1;return n?`${g.prefix||'Pen'} ${s}${n>1?' – '+(g.prefix||'Pen')+' '+(s+n-1):''}`:'';};
    UI.drawer({head:drawerHead('layers',park.name+' · Add pens','Generate pens'),cls:'idr',
      body:`<section class="isec"><div class="isec-b" style="border:0"><div class="fgrid">${num('n','Number of pens')}${num('start','Start number')}${fldH('Name prefix',`<input data-g="prefix" value="${esc(g.prefix)}">`)}${num('cap','Capacity per pen','animals')}${num('parts','Partitions per pen')}</div><div class="ihint mt6" data-gprev>${esc(prev())}</div></div></section>`,
      foot:`<button class="btn" data-a="drawer-close">Cancel</button><span class="sp"></span><button class="btn p" data-ggo>Generate</button>`,
      mount(dr){dr.querySelectorAll('[data-g]').forEach(i=>i.oninput=()=>{g[i.dataset.g]=i.value;dr.querySelector('[data-gprev]').textContent=prev();});
        dr.querySelector('[data-ggo]').onclick=()=>{const n=Math.min(200,Math.max(0,parseInt(g.n)||0));const cap=g.cap===''?'':parseInt(g.cap);if(cap!==''&&(isNaN(cap)||cap<0)){UI.toast('Capacity: 0 or more');return;}
          const names=new Set(pensOf(parkId).map(p=>REG.norm(p.name)));const snap=S.snap('Generate pens');let made=0,i=parseInt(g.start)||1;const np=Math.max(0,parseInt(g.parts)||0);
          while(made<n&&i<100000){const nm=(g.prefix||'Pen')+' '+i;i++;if(names.has(REG.norm(nm)))continue;const pen=S.add('pens',{parkId,name:nm,displayName:nm,capacity:cap,stageId:'',sex:'mixed',hasIcu:false,lifecycleStatus:''});
            for(let k=0;k<np;k++)S.add('partitions',{penId:pen.id,name:partName(nm,k),capacity:''});made++;}
          S.save();UI.closeDrawer();App.render();UI.toast(made+' pens added',()=>{S.undoTo(snap);App.render();});};}});
  }
  Object.assign(A,{
    'pk-toggle'(el,e){if(e.target.closest('[data-a="pk-penmenu"]'))return;const id=el.dataset.id;PARK.open.has(id)?PARK.open.delete(id):PARK.open.add(id);App.render();},
    'pk-edit'(el){parkDrawer(el.dataset.id||null);},
    'pk-add'(el){const id=el.dataset.id;const park=S.get('parks',id);UI.menu(el,[
      {label:'Add one',icon:'plus',run:()=>{const n=S.snap('Pen');const nm='Pen '+(pensOf(id).length+1);const p=S.add('pens',{parkId:id,name:nm,displayName:nm,capacity:'',stageId:'',sex:'mixed',hasIcu:false,lifecycleStatus:''});S.save();App.render();penDrawer(p.id);UI.toast('Added '+nm,()=>{S.undoTo(n);App.render();});}},
      {label:'Generate…',icon:'layers',run:()=>generateDrawer(id)},
      {label:'Paste from sheet',icon:'sheet',run:()=>Sheet.openEntry('pens',null,[{park:park.name}])}]);},
    'pk-grid'(el){Sheet.openEntry('pens',pensOf(el.dataset.id).map(p=>p.id));},
    'pk-editpen'(el,e){e.stopPropagation();penDrawer(el.dataset.id);},
    'pk-addpart'(el,e){e.stopPropagation();penDrawer(el.dataset.id,{addPart:true});},
    'pk-penmenu'(el,e){e.stopPropagation();const pen=S.get('pens',el.dataset.id);UI.menu(el,[
      {label:'Edit pen',icon:'edit-3',run:()=>penDrawer(pen.id)},
      {label:'Duplicate',icon:'copy',run:()=>{const n=S.snap('Duplicate pen');let nm=pen.name+' copy',k=2;while(pensOf(pen.parkId).some(x=>REG.norm(x.name)===REG.norm(nm)))nm=pen.name+' copy '+(k++);
        const c=S.add('pens',Object.assign({},pen,{id:undefined,name:nm,displayName:nm}));delete c.id;c.id=S.uid('pen');
        S.active('partitions').filter(t=>t.penId===pen.id).forEach((t,j)=>S.add('partitions',{penId:c.id,name:partName(nm,j),capacity:t.capacity}));S.save();App.render();UI.toast('Duplicated as '+nm,()=>{S.undoTo(n);App.render();});}},
      '-',{label:'Archive',icon:'archive',danger:true,run:()=>{if(List.blockToast&&List.blockToast('pens',[pen.id]))return;UI.undoable('Archived '+pen.name,()=>{pen.status='archived';S.active('partitions').filter(t=>t.penId===pen.id).forEach(t=>t.status='archived');App.render();});}}]);}
  });

  /* ---------- animal types editor (many species together) ---------- */
  let ED=null;
  function blockFrom(s){
    const of=c=>S.active(c).filter(x=>x.speciesId===s.id);
    return {id:s.id,name:s.name,code:s.code||'',breeds:of('breeds').map(b=>({id:b.id,name:b.name})),
      stages:of('stages').map(t=>({id:t.id,code:t.code,name:t.name,fromD:t.fromD,toD:t.toD,sex:t.sex})),
      tags:of('tags').filter(t=>t.kind!=='group').map(t=>({id:t.id,name:t.name})),groups:of('tags').filter(t=>t.kind==='group').map(t=>({id:t.id,name:t.name}))};
  }
  const blank=()=>({name:'',code:'',breeds:[],stages:[],tags:[],groups:[]});
  function editorDraft(ids){
    if(ED&&ED.key===ids)return ED;
    let blocks;
    if(ids==='new')blocks=[blank()];
    else if(ids==='all')blocks=S.active('species').map(blockFrom);
    else blocks=ids.split(',').map(id=>S.get('species',id)).filter(Boolean).map(blockFrom);
    ED={key:ids,blocks,removed:[],err:{},dirty:false,pending:{}};
    return ED;
  }
  function chipField(bi,field,items,ph,tone){
    return `<div class="chipin" data-chipbox="${bi}" data-f="${field}">${items.map((x,i)=>`<span class="tag t-${tone||'ok'}">${esc(x.name)}<button data-a="ed-chipdel" data-b="${bi}" data-f="${field}" data-i="${i}" aria-label="Remove ${esc(x.name)}">×</button></span>`).join('')}
      <input data-chipin="${bi}" data-f="${field}" placeholder="${esc(ph)}" aria-label="${esc(ph)}"></div>`;
  }
  function editorPage(ids){
    const d=editorDraft(ids); const E=d.err;
    const er=k=>E[k]?`<div class="ferr">${esc(E[k])}</div>`:'';
    return `<div class="phead"><div><div class="crumb">Configuration / <a href="#/configuration/items/species">Items and settings</a> / <b>Animal types</b></div><h1>${ids==='new'?'Add species':'Edit species'}</h1></div><div class="sp"></div>
      <button class="btn" data-a="ed-cancel">Cancel</button><button class="btn p" data-a="ed-save">${ic('check')}Save ${d.blocks.length} species</button></div>
      <div class="stack">${d.blocks.map((b,bi)=>`<section class="spblk">
        <div class="hd"><div style="flex:1;min-width:160px"><input class="inl" style="width:100%;font-size:15px;font-weight:700" data-ed="${bi}" data-k="name" value="${esc(b.name)}" placeholder="Species" aria-label="Species name">${er(bi+'.name')}</div>
          <input class="inl" style="width:110px" data-ed="${bi}" data-k="code" value="${esc(b.code)}" placeholder="Code" aria-label="Code">
          <button class="btn sm" data-a="ed-copystages" data-b="${bi}">${ic('copy')}Copy stages</button>
          <button class="btn icon gh" data-a="ed-remove" data-b="${bi}" aria-label="Remove species">${ic('x')}</button></div>
        <div class="bd"><div class="fgrid">
          <div class="fld full"><label>Breeds <span class="muted">${b.breeds.length}</span></label>${chipField(bi,'breeds',b.breeds,'Add breed')}</div>
          <div class="fld"><label>Pen tags</label>${chipField(bi,'tags',b.tags,'Add pen tag','mut')}</div>
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
  const STAGE_COLS=['code','name','fromD','toD','sex'];
  function editorMount(root){
    const d=ED; if(!d)return;
    root.querySelectorAll('[data-ed]').forEach(i=>i.oninput=()=>{d.blocks[+i.dataset.ed][i.dataset.k]=i.value;d.dirty=true;});
    root.querySelectorAll('[data-es]').forEach(i=>{
      i.oninput=i.onchange=()=>{d.blocks[+i.dataset.es].stages[+i.dataset.s][i.dataset.k]=i.value;d.dirty=true;};
      i.onpaste=e=>{const t=(e.clipboardData||window.clipboardData).getData('text');if(!/[\t\n]/.test(t.replace(/\n$/,'')))return;e.preventDefault();
        const b=d.blocks[+i.dataset.es],r0=+i.dataset.s,c0=STAGE_COLS.indexOf(i.dataset.k),rows=parseTSV(t);
        rows.forEach((r,ri)=>{let st=b.stages[r0+ri];if(!st){st={code:'',name:'',fromD:'',toD:'',sex:'any'};b.stages.push(st);}
          r.forEach((v,ci)=>{const k=STAGE_COLS[c0+ci];v=v.trim();if(!k)return;st[k]=k==='sex'?(['female','male'].includes(v.toLowerCase())?v.toLowerCase():'any'):v;});});
        d.dirty=true;App.render();UI.toast(rows.length+' stages pasted');};
    });
    /* chip inputs: split on separators as text arrives; the remainder stays typed; focus is restored synchronously so no keystroke is lost */
    root.querySelectorAll('[data-chipin]').forEach(i=>{
      const bi=+i.dataset.chipin,f=i.dataset.f,pk=bi+'.'+f;
      if(d.pending[pk]){i.value=d.pending[pk];}
      const commit=(vals,rest)=>{const b=d.blocks[bi];let added=0;
        vals.map(v=>v.trim()).filter(Boolean).forEach(v=>{if(!b[f].some(x=>REG.norm(x.name)===REG.norm(v))){b[f].push({name:v});added++;}});
        d.pending[pk]=rest;d.dirty=true;
        const caret=rest.length;App.render();
        const n=document.querySelector(`[data-chipin="${bi}"][data-f="${f}"]`);if(n){n.focus();n.setSelectionRange(caret,caret);}
        return added;};
      i.oninput=()=>{if(/^\s/.test(i.value))i.value=i.value.replace(/^\s+/,'');const v=i.value;d.pending[pk]=v;if(v.trim())d.dirty=true;
        if(/[,;\n\t]/.test(v)){const parts=v.split(/[,;\n\t]/);const rest=parts.pop().replace(/^\s+/,'');commit(parts,rest);}};
      i.onkeydown=e=>{if(e.key==='Enter'){e.preventDefault();if(i.value.trim())commit([i.value],'');}
        else if(e.key==='Backspace'&&!i.value&&d.blocks[bi][f].length){d.blocks[bi][f].pop();d.dirty=true;commit([],'');}};
      /* half-typed text stays in d.pending and is added on save */
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
        x.b.stages.forEach(s=>{if(!t.stages.some(y=>REG.norm(y.code)===REG.norm(s.code)))t.stages.push({code:s.code,name:s.name,fromD:s.fromD,toD:s.toD,sex:s.sex});});ED.dirty=true;App.render();}})):[{label:'No stages to copy',run(){}}]);},
    'ed-save'(){saveEditor();}
  });
  function saveEditor(target){
      const d=ED; d.err={};
      Object.entries(d.pending).forEach(([pk,v])=>{v=(v||'').trim();if(!v)return;const [bi,f]=pk.split('.');const b=d.blocks[+bi];if(b&&!b[f].some(x=>REG.norm(x.name)===REG.norm(v)))b[f].push({name:v});d.pending[pk]='';});
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
      if(Object.keys(d.err).length){App.render();UI.toast('Fix highlighted fields');return false;}
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
        sync('tags',b.tags,it=>({name:it.name,kind:'tag'}),x=>x.kind!=='group');
        sync('tags',b.groups,it=>({name:it.name,kind:'group'}),x=>x.kind==='group');
        sync('stages',b.stages,it=>({code:(it.code||it.name).trim().toUpperCase(),name:(it.name||it.code).trim(),fromD:it.fromD===''?'':Number(it.fromD),toD:it.toD===''?'':Number(it.toD),sex:it.sex||'any'}));
      });
      S.save(); ED=null; App.leave(target||'#/configuration/items/species',true);
      UI.toast('Saved '+d.blocks.length+' species',()=>{S.undoTo(n);App.render();});
      return true;
  }

  /* ---------- workbook menu ---------- */
  A['wb-menu']=el=>UI.menu(el,[
    {label:'Import workbook',icon:'upload',run:()=>{IO.start('workbook');}},
    {label:'Template (.xlsx)',icon:'download',run:()=>IO.workbookTemplate('xlsx')},
    {label:'Export all (.xlsx)',icon:'download',run:()=>IO.workbookTemplate('xlsx',true)}]);

  /* unsaved-draft guard for park page and animal types editor */
  P.dirty=function(){
    if(ED&&(ED.dirty||Object.values(ED.pending).some(v=>v&&v.trim())))return {discard(){ED=null;},save:t=>saveEditor(t)};
    const g=Sheet.cur,at=App._lastPath||location.hash;
    if(g&&at.includes('/sheet/')&&!g.summary&&Sheet.isDirty(g))return {discard(){Sheet.cur=null;}};
    return null;
  };

  /* ---------- router entry ---------- */
  P.render=function(parts){
    let key=parts[0]||'parks';
    const legacy={places:parts[1]||'parks','animal-types':parts[1]&&!['edit'].includes(parts[1])?parts[1]:'species',animals:'animals',items:'items',people:parts[1]==='approvers'?'approvalChains':parts[1]||'people',approvers:'approvalChains',settings:'settings'};
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
    if(key==='vendors'||key==='trucks'){location.replace('#/procurement/vendors');return {html:''};}
    if(key==='park'||(key==='places'&&parts[1]==='park'))return {html:parkPage((key==='park'?parts[1]:parts[2])||'new'),mount:parkMount};
    if(key==='animal-types'&&parts[1]&&(parts[1]==='new'||parts[1]==='all'||parts[1]==='edit'||S.get('species',parts[1].split(',')[0]))){const ids=parts[1]==='edit'?(parts[2]||'new'):parts[1];return {html:editorPage(ids),mount:editorMount};}
    if(legacy[key]&&!PANELS[key])key=legacy[key];
    if(!PANELS[key])key='parks';
    PD=null; ED=null;
    return {html:header()+`<div class="cfg">${rail(key)}<div class="cfgpanel">${PANELS[key]()}</div></div>`};
  };
  window.SetupPage=P;
})();
