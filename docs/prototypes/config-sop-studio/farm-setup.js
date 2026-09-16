/* Farm and animal registers for review. No production API calls. */
const EntityRegistryModel={
 definitions:[
  {id:'parks',module:'Counts',name:'Parks',singular:'park',hint:'Top-level farm or site. Existing CBE and CPT are practice copies.',fields:['code','notes']},
  {id:'pens',module:'Counts',name:'Pens',singular:'pen',parent:'parks',hint:'Animal living area inside a park. Capacity is proposed; production currently has pen registration without capacity.',fields:['code','capacity','order','notes']},
  {id:'partitions',module:'Counts',name:'Pen partitions',singular:'partition',parent:'pens',hint:'A split inside a pen, used by feed, vaccination, weighing and task planning when work is done per partition.',fields:['code','capacity','order','notes']},
  {id:'species',module:'Counts',name:'Animal species',singular:'animal species',hint:'Animal types: goat, sheep, cow, buffalo, prawn, fish, crab or future species.',fields:['code','notes']},
  {id:'breeds',module:'Counts',name:'Breed names',singular:'breed',parent:'species',hint:'Breed names under each species.',fields:['code','notes']},
  {id:'lifecycle_stages',module:'Counts',name:'Lifecycle stages',singular:'stage',parent:'species',hint:'User-defined stages per species, such as K0, K1, K2, K3, Fattening or grow-out.',fields:['code','order','notes']},
  {id:'shed_tags',module:'Counts',name:'Shed tags / stage tags',singular:'shed tag',parent:'species',hint:'Shed/stage labels per species, such as warm-up, ICU kids or bucks.',fields:['code','stage','notes']},
  {id:'animal_tags',module:'Counts',name:'Animal tags / groups',singular:'tag or group',parent:'species',hint:'Business groups such as breeding or fattening.',fields:['code','notes']},
  {id:'animals',module:'Counts',name:'Animals',singular:'animal',parent:'pens',hint:'Actual animal identity records. Species controls available breed, stage and tag choices.',fields:['tag1','tag2','species','sex','breed','stage','shedTag','weightKg','notes']}
 ],
 seed(){
  return {records:{
   parks:[{id:'practice-cbe',name:'CBE',code:'CBE',active:true},{id:'practice-cpt',name:'CPT',code:'CPT',active:true}],
   pens:[],partitions:[],
   species:[{id:'species-goat',name:'Goat',code:'GOAT',active:true},{id:'species-sheep',name:'Sheep',code:'SHEEP',active:true},{id:'species-cow',name:'Cow',code:'COW',active:true},{id:'species-buffalo',name:'Buffalo',code:'BUFFALO',active:true},{id:'species-prawn',name:'Prawn',code:'PRAWN',active:true},{id:'species-fish',name:'Fish',code:'FISH',active:true},{id:'species-crab',name:'Crab',code:'CRAB',active:true}],
   breeds:[{id:'breed-boer',name:'Boer',code:'BOER',parentId:'species-goat',active:true},{id:'breed-local-goat',name:'Local goat',code:'LOCAL-GOAT',parentId:'species-goat',active:true},{id:'breed-local-sheep',name:'Local sheep',code:'LOCAL-SHEEP',parentId:'species-sheep',active:true},{id:'breed-murrah',name:'Murrah buffalo',code:'MURRAH',parentId:'species-buffalo',active:true},{id:'breed-country-cow',name:'Country cow',code:'COUNTRY-COW',parentId:'species-cow',active:true}],
   lifecycle_stages:[{id:'stage-k0',name:'K0',code:'K0',parentId:'species-goat',order:0,active:true,notes:'First two days after birth; mother and kid together.'},{id:'stage-k1',name:'K1',code:'K1',parentId:'species-goat',order:1,active:true,notes:'First week kid class.'},{id:'stage-k2',name:'K2',code:'K2',parentId:'species-goat',order:2,active:true,notes:'After seven days.'},{id:'stage-k3',name:'K3',code:'K3',parentId:'species-goat',order:3,active:true,notes:'Weaning/growth class, around 11-12 kg average.'},{id:'stage-fattening-goat',name:'Fattening',code:'FATTENING',parentId:'species-goat',order:4,active:true,notes:'Fattening animals intended for sale.'},{id:'stage-adult-female',name:'Adult female',code:'ADULT-F',parentId:'species-goat',order:5,active:true},{id:'stage-buck',name:'Adult male / buck',code:'BUCK',parentId:'species-goat',order:6,active:true},{id:'stage-fish-growout',name:'Grow-out',code:'GROWOUT',parentId:'species-fish',order:1,active:true,notes:'Example aquatic lifecycle stage; editable or removable.'}],
   shed_tags:[{id:'shedtag-k0',name:'K0',code:'K0',parentId:'species-goat',stage:'stage-k0',active:true,notes:'Birth shed/counting tag.'},{id:'shedtag-k1',name:'K1',code:'K1',parentId:'species-goat',stage:'stage-k1',active:true},{id:'shedtag-k2',name:'K2',code:'K2',parentId:'species-goat',stage:'stage-k2',active:true},{id:'shedtag-k3',name:'K3',code:'K3',parentId:'species-goat',stage:'stage-k3',active:true},{id:'shedtag-fattening-goat',name:'Fattening',code:'FATTENING',parentId:'species-goat',stage:'stage-fattening-goat',active:true},{id:'shedtag-icu-kids',name:'ICU kids',code:'ICU-KIDS',parentId:'species-goat',stage:'stage-k1',active:true},{id:'shedtag-warmup',name:'Warm-up',code:'WARM-UP',parentId:'species-goat',stage:'stage-fattening-goat',active:true,notes:'New procurement feed-transition group.'},{id:'shedtag-nonpregnant',name:'Non-pregnant females',code:'NPF',parentId:'species-goat',stage:'stage-adult-female',active:true},{id:'shedtag-bucks',name:'Bucks',code:'BUCKS',parentId:'species-goat',stage:'stage-buck',active:true}],
   animal_tags:[{id:'tag-breeding',name:'Breeding',code:'BREEDING',parentId:'species-goat',active:true},{id:'tag-fattening',name:'Fattening',code:'FATTENING',parentId:'species-goat',active:true}],
   animals:[]
  }};
 },
 def(kind){const d=this.definitions.find(x=>x.id===kind);if(!d)throw Error('Unknown register.');return d;},
 rows(data,kind){data.records??={};data.records[kind]??=[];return data.records[kind];},
 save(data,kind,input){
  const def=this.def(kind),rows=this.rows(data,kind),old=rows.find(r=>r.id===input.id),name=String(input.name||'').trim(),code=String(input.code||'').trim();
  if(!name)throw Error('Enter a name.');
  if(def.parent&&!this.rows(data,def.parent).some(p=>p.id===input.parentId&&p.active))throw Error('Choose an active '+this.def(def.parent).singular+'.');
  if(old&&def.parent&&old.parentId!==input.parentId&&Number(old.usedCount||old.animalCount||0)>0)throw Error('Move existing linked records before changing the parent.');
  const scope=r=>def.parent?r.parentId===input.parentId:true;
  if(rows.some(r=>r.id!==input.id&&scope(r)&&(r.name.trim().toLowerCase()===name.toLowerCase()||(code&&String(r.code||'').toLowerCase()===code.toLowerCase()))))throw Error('Use a unique name and code within this register.');
  const record={...old,id:old?.id||crypto.randomUUID(),name,code,active:old?.active??true,parentId:def.parent?input.parentId:null,usedCount:old?.usedCount||0,animalCount:old?.animalCount||0};
  for(const f of def.fields||[]){
   if(['code'].includes(f))continue;
   if(['capacity','order','quantity','quantityKg','weightKg'].includes(f)){
    const raw=input[f],empty=raw==null||String(raw).trim()==='';
    record[f]=empty?null:Number(raw);
    if(record[f]!==null&&(!Number.isFinite(record[f])||record[f]<0))throw Error(labelForField(f)+' must be zero or more, or left empty.');
    if(f==='capacity'&&record[f]!==null&&record[f]<Number(old?.animalCount||0))throw Error('Capacity cannot be below the animals already linked here.');
   }else record[f]=String(input[f]||'').trim();
  }
  old?Object.assign(old,record):rows.push(record);return record;
 },
 archive(data,kind,id){
  const def=this.def(kind),r=this.rows(data,kind).find(x=>x.id===id);if(!r)throw Error('Record not found.');
  const child=this.definitions.find(d=>d.parent===kind);
  if(child&&this.rows(data,child.id).some(x=>x.parentId===id&&x.active))throw Error('Archive linked '+child.name.toLowerCase()+' first.');
  if(Number(r.usedCount||r.animalCount||0)>0)throw Error('Only an unused or empty record can be archived.');
  r.active=false;
 }
};
const FarmSetupModel={
 seed(){const data=EntityRegistryModel.seed();return {parks:data.records.parks,pens:data.records.pens,records:data.records};},
 save(data,kind,input){data.records??={parks:data.parks||[],pens:data.pens||[]};return EntityRegistryModel.save(data,kind==='park'?'parks':'pens',{...input,parentId:input.parkId});},
 archive(data,kind,id){data.records??={parks:data.parks||[],pens:data.pens||[]};return EntityRegistryModel.archive(data,kind==='park'?'parks':'pens',id);}
};
function labelForField(f){return ({tag1:'Primary tag',tag2:'Second tag',weightKg:'Weight',quantityKg:'Quantity',approvalRole:'Approval role',shedTag:'Shed tag'}[f]||f.replace(/([A-Z])/g,' $1')).replace(/^./,c=>c.toUpperCase())}
function mergeSeedRows(records,kind,seeded){records[kind]??=[];const rows=records[kind];for(const seed of seeded[kind]||[]){const byId=rows.find(r=>r.id===seed.id),byCode=seed.code&&rows.find(r=>String(r.code||'').toLowerCase()===String(seed.code).toLowerCase()&&r.parentId===seed.parentId);if(!byId&&!byCode)rows.push({...seed});}}
function entityRegistryData(){if(!state.entityRegistry){state.entityRegistry=EntityRegistryModel.seed();if(state.farmSetup){state.entityRegistry.records.parks=state.farmSetup.parks||state.entityRegistry.records.parks;state.entityRegistry.records.pens=state.farmSetup.pens||[];}}const seeded=EntityRegistryModel.seed().records;state.entityRegistry.records??={};for(const def of EntityRegistryModel.definitions){state.entityRegistry.records[def.id]??=[];}for(const kind of ['species','breeds','lifecycle_stages','shed_tags','animal_tags'])mergeSeedRows(state.entityRegistry.records,kind,seeded);return state.entityRegistry}
function farmSetupData(){const d=entityRegistryData();return {parks:d.records.parks,pens:d.records.pens,records:d.records}}
function entityCounts(d,def){const rows=EntityRegistryModel.rows(d,def.id),active=rows.filter(r=>r.active).length;return `${active} active${rows.length!==active?' · '+(rows.length-active)+' archived':''}`}
function entityRegister(module='Counts'){const d=entityRegistryData(),defs=EntityRegistryModel.definitions,modules=[...new Set(defs.map(x=>x.module))],current=modules.includes(module)?module:modules[0],selected=defs.filter(x=>x.module===current);modal('Farm and animal registers',`<div class="entity-register"><div class="entity-register-head"><div><p><b>Only missing cross-feature records live here:</b> parks, pens, partitions, species, breeds, lifecycle stages, shed tags, animal groups and animals. Existing Feed, Sales, Procurement, Vaccination and Health screens stay in their own modules.</p></div><div class="entity-register-totals"><b>${defs.length}</b><span>registers</span></div></div><div class="entity-register-shell"><nav class="entity-register-modules" aria-label="Register modules">${modules.map(m=>`<button class="${m===current?'active':''}" onclick="entityRegister('${m}')"><span>${esc(m)}</span><b>${defs.filter(x=>x.module===m).length}</b></button>`).join('')}</nav><section class="entity-register-panel"><div class="entity-register-panel-head"><span class="eyebrow">${esc(current)}</span><h3>${esc(current)} registers</h3><p>${esc(entityModuleSummary(current))}</p></div><div class="entity-register-rows">${selected.map(def=>entityRegisterRow(d,def)).join('')}</div><div class="entity-existing-list"><h3>Already exists elsewhere</h3>${existingFeatureScreens().map(existingFeatureRow).join('')}</div></section></div></div>`,true)}
function entityModuleSummary(module){return ({Counts:'Farm structure, animal types, breed names, lifecycle stages, shed tags and animal identity records that other modules reference.'})[module]||'Feature-owned operational records.'}

function existingFeatureScreens(){return [
 {module:'Feed',title:'Feed Config',text:'Feed items, ration grids, rates and feed schedules already live in Feed Config.',go:"productionNavigate('Feed','Feed Config')"},
 {module:'Sales',title:'Sales pages and Sales Config',text:'Buyers, loads, farm value, market analytics and selling settings stay in Sales.',go:"productionNavigate('Sales','Sales Config')"},
 {module:'Procurement',title:'Procurement vendors and purchases',text:'Source entry, vendors, feed purchases and animal purchases stay in Procurement.',go:"productionNavigate('Procurement','Vendors')"},
 {module:'Preventive Care',title:'Vaccination plan',text:'Vaccine schedules, dose timing and published plan versions stay in Vaccination plan.',go:"productionNavigate('Preventive Care','Vaccination plan')"},
 {module:'Health',title:'Health Config',text:'Diseases, symptoms, protocols and treatment authoring stay in Health Config.',go:"productionNavigate('Health','Health Config')"}
]}
function existingFeatureRow(x){return `<article class="entity-register-row existing-screen-row" role="button" tabindex="0" onclick="closeModal();${x.go}" onkeydown="if(event.key==='Enter'||event.key===' '){event.preventDefault();closeModal();${x.go}}"><div><strong>${esc(x.title)}</strong><span>${esc(x.module)}</span></div><p>${esc(x.text)}</p></article>`}
function entityRegisterRow(data,def){const parent=def.parent?EntityRegistryModel.def(def.parent):null;return `<article class="entity-register-row" role="button" tabindex="0" onclick="entityRegisterList('${def.id}')" onkeydown="if(event.key==='Enter'||event.key===' '){event.preventDefault();entityRegisterList('${def.id}')}"><div><strong>${esc(def.name)}</strong><span>${esc(entityCounts(data,def))}${parent?' · inside '+esc(labelForRegister(parent).toLowerCase()):''}</span></div><p>${esc(def.hint)}</p></article>`}
function entityRegisterList(kind){const d=entityRegistryData(),def=EntityRegistryModel.def(kind),rows=EntityRegistryModel.rows(d,kind),parentDef=def.parent?EntityRegistryModel.def(def.parent):null;modal(def.name,`<div class="entity-register"><div class="actions"><button onclick="entityRegister()">All registers</button>${canEdit()?`<button class="primary" onclick="entityRegisterEdit('${kind}')">Add ${esc(def.singular)}</button>`:''}</div><div class="tablewrap"><table class="items-table"><thead><tr><th>Name</th>${parentDef?'<th>'+esc(labelForRegister(parentDef))+'</th>':''}<th>Details</th><th>Status</th><th></th></tr></thead><tbody>${rows.map(r=>`<tr><td><button class="item-name" onclick="entityRegisterEdit('${kind}','${r.id}')">${esc(r.name)}</button><div class="muted">${esc(r.code||'No code')}</div></td>${parentDef?`<td>${esc(entityName(d,def.parent,r.parentId))}</td>`:''}<td>${entityDetails(def,r)}</td><td><span class="status-pill ${r.active?'active':'archived'}">${r.active?'Active':'Archived'}</span></td><td>${r.active&&canEdit()?`<button onclick="entityRegisterEdit('${kind}','${r.id}')">Edit</button> <button onclick="entityArchive('${kind}','${r.id}')">Archive</button>`:''}</td></tr>`).join('')||`<tr><td colspan="${parentDef?5:4}"><div class="empty">No ${esc(def.name.toLowerCase())} added in this browser yet.</div></td></tr>`}</tbody></table></div></div>`,true)}
function entityName(data,kind,id){return EntityRegistryModel.rows(data,kind).find(r=>r.id===id)?.name||'Not selected'}
function labelForRegister(def){return def.singular.replace(/^./,c=>c.toUpperCase())}
function entityReferenceKind(field){return ({species:'species',breed:'breeds',stage:'lifecycle_stages',shedTag:'shed_tags'}[field])}
function entityValueLabel(data,field,value){const kind=entityReferenceKind(field);return kind?entityName(data,kind,value):value}
function entityDetails(def,r){const d=entityRegistryData();let fields=(def.fields||[]).filter(f=>f!=='code');if(def.id==='animals')fields=['tag1','tag2','species','breed','stage','shedTag','sex','weightKg'];return `<div class="muted">${fields.map(f=>`${esc(labelForField(f))}: ${esc(entityValueLabel(d,f,r[f])??'Not set')}`).join(' · ')||'No extra fields'}</div>`}
function entityRegisterEdit(kind,id){
 if(!canEdit())return toast('Editing is not available for this role.');
 const d=entityRegistryData(),def=EntityRegistryModel.def(kind),r=EntityRegistryModel.rows(d,kind).find(x=>x.id===id)||{},parentDef=def.parent?EntityRegistryModel.def(def.parent):null;
 const animalIdentity=kind==='animals';
 modal((id?'Edit ':'Add ')+def.singular,`<form id="entityform" onsubmit="event.preventDefault();entityRegisterSave('${kind}','${id||''}')">
  ${animalIdentity?`<div class="form-section-title">Animal identity</div><label class="field">Primary tag / RFID<input id="entity-name" required value="${esc(r.name||r.tag1||'')}"></label><label class="field">Internal code<input id="entity-code" value="${esc(r.code||'')}"></label>`:`<label class="field">Name<input id="entity-name" required value="${esc(r.name||'')}"></label><label class="field">Code<input id="entity-code" value="${esc(r.code||'')}"></label>`}
  ${parentDef?`<label class="field">${esc(parentDef.singular.replace(/^./,c=>c.toUpperCase()))}<select id="entity-parent">${EntityRegistryModel.rows(d,def.parent).filter(p=>p.active).map(p=>`<option value="${p.id}" ${p.id===r.parentId?'selected':''}>${esc(p.name)}</option>`).join('')}</select></label>`:''}
  ${animalIdentity?'<div class="form-section-title">Animal classification</div>':''}
  ${(def.fields||[]).filter(f=>f!=='code'&&!(animalIdentity&&f==='tag1')).map(f=>entityField(f,r[f],kind,r)).join('')}
  <button type="button" onclick="entityRegisterList('${kind}')">Back</button> <button class="primary" type="submit">Save ${esc(def.singular)}</button>
 </form>`,true);
 if(animalIdentity)entityRefreshAnimalDependentFields();
}
function entityField(f,value,kind,record={}){
 const numeric=['capacity','order','quantity','quantityKg','weightKg'].includes(f),label=labelForField(f),ref=entityReferenceKind(f);
 if(ref){
  const d=entityRegistryData();
  const speciesId=kind==='animals'?(record.species||''):'';
  let rows=EntityRegistryModel.rows(d,ref).filter(r=>r.active);
  if(kind==='animals'&&['breed','stage','shedTag'].includes(f)&&speciesId)rows=rows.filter(r=>r.parentId===speciesId);
  const change=f==='species'&&kind==='animals'?' onchange="entityRefreshAnimalDependentFields()"':'';
  return `<label class="field">${label}<select id="entity-${f}" data-current="${esc(value||'')}"${change}><option value="">Not selected</option>${rows.map(r=>`<option value="${esc(r.id)}" ${r.id===value?'selected':''}>${esc(r.name)}</option>`).join('')}</select></label>`;
 }
 if(f==='sex')return `<label class="field">${label}<select id="entity-${f}"><option value="">Not selected</option>${['Female','Male','Unknown'].map(x=>`<option value="${x.toLowerCase()}" ${String(value||'')===x.toLowerCase()?'selected':''}>${x}</option>`).join('')}</select></label>`;
 if(f==='notes')return `<label class="field">${label}<textarea id="entity-${f}">${esc(value||'')}</textarea></label>`;
 return `<label class="field">${label}<input id="entity-${f}" ${numeric?'type="number" min="0" step="0.01"':''} value="${esc(value??'')}"></label>`;
}
function entityRefreshAnimalDependentFields(){
 const species=$('#entity-species')?.value||'';
 for(const [field,kind] of [['breed','breeds'],['stage','lifecycle_stages'],['shedTag','shed_tags']]){
  const select=$('#entity-'+field);if(!select)continue;
  const current=select.value||select.dataset.current||'';
  const rows=EntityRegistryModel.rows(entityRegistryData(),kind).filter(r=>r.active&&(!species||r.parentId===species));
  select.innerHTML='<option value="">Not selected</option>'+rows.map(r=>`<option value="${esc(r.id)}" ${r.id===current?'selected':''}>${esc(r.name)}</option>`).join('');
  if(current&&!rows.some(r=>r.id===current))select.value='';
 }
}
function entityRegisterSave(kind,id){
 if(!canEdit())return toast('Editing is not available for this role.');
 const def=EntityRegistryModel.def(kind),input={id,name:$('#entity-name').value,code:$('#entity-code').value,parentId:def.parent?$('#entity-parent').value:null};
 for(const f of def.fields||[])if(f!=='code')input[f]=$('#entity-'+f)?.value||'';
 if(kind==='animals'){
  input.tag1=input.name;
  const species=input.species;
  for(const [field,refKind] of [['breed','breeds'],['stage','lifecycle_stages'],['shedTag','shed_tags']]){
   if(input[field]&&!EntityRegistryModel.rows(entityRegistryData(),refKind).some(r=>r.id===input[field]&&r.parentId===species&&r.active))throw Error(labelForField(field)+' must belong to the selected species.');
  }
 }
 try{EntityRegistryModel.save(entityRegistryData(),kind,input);persist();entityRegisterList(kind)}catch(e){toast(e.message)}
}
function entityArchive(kind,id){if(!canEdit())return toast('Editing is not available for this role.');try{EntityRegistryModel.archive(entityRegistryData(),kind,id);persist();entityRegisterList(kind)}catch(e){toast(e.message)}}
function farmSetup(){entityRegisterList('parks')}
function farmSetupEdit(kind,id){entityRegisterEdit(kind==='park'?'parks':'pens',id)}
function farmSetupSave(kind,id){entityRegisterSave(kind==='park'?'parks':'pens',id)}
function farmSetupArchive(kind,id){entityArchive(kind==='park'?'parks':'pens',id)}
let configWorkspaceSection='animal_lists',configWorkspaceRegister='species';
function openConfigWorkspace(section='farm',register){configWorkspaceSection=section;if(register)configWorkspaceRegister=register;renderItems();setTimeout(()=>document.querySelector('#configuration-workspace')?.scrollIntoView({block:'start'}),0)}
function selectConfigRegister(kind){configWorkspaceSection='farm';configWorkspaceRegister=kind;renderItems()}
const farmSetupBaseItems=renderItems;renderItems=function(){farmSetupBaseItems();const target=$('.items-heading');if(target&&!$('#farm-setup-entry'))target.insertAdjacentHTML('beforeend',`<button id="farm-setup-entry" onclick="openConfigWorkspace('farm')">Configure farm setup</button>`)};

function entityRegisterUses(def){
 const uses={
  parks:['Counts analytics','Weighing pen assignment','Feed and vaccination scope'],
  pens:['Weighing Session','Feed Distribution','Vaccination drives','Health tasks'],
  partitions:['Feed planning','Vaccination plan scope','Weighing by partition'],
  species:['Any species setup','Breed setup','Animal register'],
  breeds:['Animal register','Procurement source selection','Sales and valuation filters'],
  lifecycle_stages:['User-defined lifecycle classes','Sales valuation settings','Eligibility and lifecycle reports'],
  shed_tags:['Sheds setup','Counts breakdown','Feed ration rules','Weighing and vaccination scope'],
  animal_tags:['Breeding/fattening groups','Procurement routing','Sales and feed filters'],
  animals:['Herd Register','Procurement approved animals','Health and vaccination tasks'],
 };
 return uses[def.id]||['Feature SOPs'];
}
function configWorkspaceTabs(){return [
 {id:'animal_lists',label:'Animal lists',count:5},
 {id:'farm',label:'Farm places',count:3},
 {id:'animals',label:'Animal identities',count:1},
 {id:'feed',label:'Feed setup',count:4},
 {id:'catalogue',label:'Catalogue',count:3}
].map(t=>`<button class="${configWorkspaceSection===t.id?'active':''}" onclick="openConfigWorkspace('${t.id}')"><span>${esc(t.label)}</span><b>${esc(String(t.count))}</b></button>`).join('')}
function configWorkspaceInline(){return `<section class="config-workspace config-hub" id="configuration-workspace"><div class="config-workspace-head"><div><span class="eyebrow">Configuration workspace</span><h2>Business setup</h2><p>Common records used by existing modules.</p></div><div class="config-workspace-total"><b>${EntityRegistryModel.definitions.length}</b><span>registers</span></div></div><div class="config-hub-tabs" role="tablist">${configWorkspaceTabs()}</div><div class="config-workspace-panel">${configWorkspacePanel()}</div></section>`}
function configWorkspacePanel(){return (configWorkspaceSection==='feed'?configFeedPanel:configWorkspaceSection==='catalogue'?configCataloguePanel:configWorkspaceSection==='animals'?configAnimalsPanel:configFarmPanel)(configWorkspaceSection)}
function configFarmPanel(section='animal_lists'){const d=entityRegistryData(),groups={animal_lists:{title:'Animal setup lists',eyebrow:'Animal setup',text:'Create species first. Breed, stage and tag records sit under a species.',ids:['species','breeds','lifecycle_stages','shed_tags','animal_tags'],fallback:'species'},farm:{title:'Farm places',eyebrow:'Counts',text:'Parks, pens and pen partitions are physical locations used by modules.',ids:['parks','pens','partitions'],fallback:'parks'}};const group=groups[section]||groups.animal_lists;if(!group.ids.includes(configWorkspaceRegister))configWorkspaceRegister=group.fallback;const defs=group.ids.map(id=>EntityRegistryModel.def(id)),def=EntityRegistryModel.def(configWorkspaceRegister);return `<div class="config-panel-title"><span class="eyebrow">${esc(group.eyebrow)}</span><h3>${esc(group.title)}</h3><p>${esc(group.text)}</p></div><div class="register-card-grid register-card-grid-compact">${defs.map(x=>`<button class="register-tile ${x.id===configWorkspaceRegister?'active':''}" onclick="selectConfigRegister('${x.id}')"><span>${esc(entityCounts(d,x))}</span><strong>${esc(x.name)}</strong></button>`).join('')}</div><section class="register-detail-modern">${configRegisterDetail(def)}</section>`}
function configAnimalsPanel(){configWorkspaceRegister='animals';const def=EntityRegistryModel.def('animals');return `<div class="config-panel-title"><span class="eyebrow">Herd register</span><h3>Animal identities</h3><p>One row per animal. It uses the lists already created above.</p></div><section class="register-detail-modern">${configRegisterDetail(def)}</section>`}
function configRegisterDetail(def){const d=entityRegistryData(),rows=EntityRegistryModel.rows(d,def.id),parentDef=def.parent?EntityRegistryModel.def(def.parent):null;return `<div class="register-detail-head"><div><h4>${esc(def.name)}</h4><p>${esc(def.hint)}</p></div>${canEdit()?`<div class="actions"><button class="primary" onclick="entityRegisterEdit('${def.id}')">Add ${esc(def.singular)}</button>${['species','breeds','lifecycle_stages','shed_tags','animal_tags'].includes(def.id)?`<button class="secondary" onclick="bulkAnimalSetup()">Bulk setup</button>`:''}</div>`:''}</div><div class="clean-table"><table><thead><tr><th>Name</th>${parentDef?`<th>${esc(labelForRegister(parentDef))}</th>`:''}<th>Details</th><th>Status</th></tr></thead><tbody>${rows.map(r=>`<tr class="item-click-row" tabindex="0" onclick="entityRegisterEdit('${def.id}','${r.id}')" onkeydown="if(event.key==='Enter'||event.key===' '){event.preventDefault();entityRegisterEdit('${def.id}','${r.id}')}"><td><b>${esc(r.name)}</b><span>${esc(r.code||'No code')}</span></td>${parentDef?`<td>${esc(entityName(d,def.parent,r.parentId))}</td>`:''}<td>${entityDetails(def,r)}</td><td><span class="status-pill ${r.active?'active':'archived'}">${r.active?'Active':'Archived'}</span></td></tr>`).join('')||`<tr><td colspan="${parentDef?4:3}"><div class="empty">No ${esc(def.name.toLowerCase())} added yet.</div></td></tr>`}</tbody></table></div>`}

function listToLines(rows,kind){const d=entityRegistryData();return rows.map(r=>{const parent=EntityRegistryModel.def(kind).parent?entityName(d,EntityRegistryModel.def(kind).parent,r.parentId)+' / ':'';return parent+r.name}).join('\n')}
function bulkAnimalSetup(){
 if(!canEdit())return toast('Editing is not available for this role.');
 const d=entityRegistryData();
 modal('Bulk animal setup',`<form id="bulk-animal-setup" onsubmit="event.preventDefault();bulkAnimalSetupSave()"><div class="bulk-setup-grid">
  <section class="bulk-setup-card"><h3>Species</h3><p>One per line. Example: Goat, Sheep, Cow.</p><textarea id="bulk-species">${esc(listToLines(EntityRegistryModel.rows(d,'species'),'species'))}</textarea></section>
  <section class="bulk-setup-card"><h3>Breeds</h3><p>Format: Species / Breed. Example: Goat / Beetal.</p><textarea id="bulk-breeds">${esc(listToLines(EntityRegistryModel.rows(d,'breeds'),'breeds'))}</textarea></section>
  <section class="bulk-setup-card"><h3>Lifecycle stages</h3><p>Format: Species / Stage. Example: Goat / K1.</p><textarea id="bulk-stages">${esc(listToLines(EntityRegistryModel.rows(d,'lifecycle_stages'),'lifecycle_stages'))}</textarea></section>
  <section class="bulk-setup-card"><h3>Shed tags and groups</h3><p>Format: Species / Tag. Example: Sheep / Warm-up.</p><textarea id="bulk-tags">${esc([...EntityRegistryModel.rows(d,'shed_tags'),...EntityRegistryModel.rows(d,'animal_tags')].map(r=>entityName(d,'species',r.parentId)+' / '+r.name).join('\n'))}</textarea></section>
 </div><div class="bulk-actions"><button type="button" onclick="closeModal()">Cancel</button><button class="primary" type="submit">Save setup</button></div></form>`,true)
}
function parseBulkLines(value){return String(value||'').split('\n').map(x=>x.trim()).filter(Boolean)}
function codeFromName(name){return String(name||'').trim().toUpperCase().replace(/[^A-Z0-9]+/g,'-').replace(/^-|-$/g,'')}
function findOrCreateSpecies(data,name){const rows=EntityRegistryModel.rows(data,'species'),existing=rows.find(r=>r.name.toLowerCase()===name.toLowerCase()&&r.active);if(existing)return existing;return EntityRegistryModel.save(data,'species',{name,code:codeFromName(name)})}
function saveChildByLine(data,kind,line){const parts=line.split('/').map(x=>x.trim()).filter(Boolean);if(parts.length<2)throw Error('Use "Species / Name" format for '+EntityRegistryModel.def(kind).name+'.');const species=findOrCreateSpecies(data,parts[0]);return EntityRegistryModel.save(data,kind,{name:parts.slice(1).join(' / '),code:codeFromName(parts.slice(1).join(' / ')),parentId:species.id})}
function bulkAnimalSetupSave(){
 try{
  const d=entityRegistryData();
  for(const name of parseBulkLines($('#bulk-species').value))findOrCreateSpecies(d,name);
  for(const line of parseBulkLines($('#bulk-breeds').value))saveChildByLine(d,'breeds',line);
  for(const line of parseBulkLines($('#bulk-stages').value))saveChildByLine(d,'lifecycle_stages',line);
  for(const line of parseBulkLines($('#bulk-tags').value))saveChildByLine(d,'shed_tags',line);
  persist();closeModal();renderItems();toast('Animal setup saved.');
 }catch(e){toast(e.message)}
}

function configCataloguePanel(){const rows=[['Animal identities','Herd register','Goat ID 100 / RFID record'],['Animal types','Species list','Goat / Sheep / Cow / Buffalo / Fish / Prawn / Crab'],['Breeds','Goat','Beetal / Boer / Indian'],['Lifecycle stages','Goat','K0 / K1 / K2 / K3 / Fattening'],['Shed tags','Goat','Warm-up / ICU kids / Bucks'],['Feed items','Dry feed','Dry Masoor Bhusa'],['Medicines','Antibiotics','Approved medicine item'],['Business partners','Vendors','Seller record'],['Business partners','Buyers','Buyer record']];return `<div class="config-panel-title"><span class="eyebrow">Catalogue</span><h3>Category → subcategory → item</h3><p>Modules use records; modules are not categories.</p></div><div class="clean-table feed-preview"><table><thead><tr><th>Category</th><th>Subcategory</th><th>Item / record</th></tr></thead><tbody>${rows.map(r=>`<tr><td>${esc(r[0])}</td><td>${esc(r[1])}</td><td>${esc(r[2])}</td></tr>`).join('')}</tbody></table></div>`}
function configFeedPanel(){const rows=[['Ration rates','Park, breed, pen tag, feed item, grams/head/day and effective dates.'],['Feed items','Feed item name, energy, dry matter, wastage, display order and active status.'],['Feeding schedule','Normal and experiment schedules: direction, correction and transport times.'],['Feed / water removal','Existing cut-off setting used by Feed and Weighing.']];return `<div class="config-panel-title"><span class="eyebrow">Feed</span><h3>Feed setup</h3><p>Existing Feed Config fields, shown here as a summary.</p></div><div class="feed-config-grid">${rows.map(r=>`<article><h4>${esc(r[0])}</h4><p>${esc(r[1])}</p></article>`).join('')}</div><div class="clean-table feed-preview"><table><thead><tr><th>Area</th><th>Existing records/fields</th><th>Owner</th></tr></thead><tbody><tr><td>Feed items</td><td>Dry Masoor Bhusa, Mesha Kids Concentrate, Mesha Adult Concentrate; nutrition factors can stay blank when not measured.</td><td>Feed Config</td></tr><tr><td>Ration rates</td><td>Park + breed + pen tag + feed item + grams/head/day + effective dates.</td><td>Feed Config rate grid</td></tr><tr><td>Schedules</td><td>Normal and experiment feeding timings.</td><td>Feed Config schedule section</td></tr></tbody></table></div>`}
function configModulePanel(section){const map={sales:['Sales','Buyers, loads, farm value, market analytics and sale eligibility settings stay in Sales. Do not copy those screens into this setup page.','Sales Config'],procurement:['Procurement','Vendors, source entry, feed purchases and animal purchases stay in Procurement.','Vendors'],health:['Health','Diseases, symptoms, protocols and treatment authoring stay in Health Config.','Health Config']};const m=map[section]||map.sales;return `<div class="config-panel-title"><span class="eyebrow">${esc(m[0])}</span><h3>${esc(m[0])} is not duplicated here</h3><p>${esc(m[1])}</p></div><div class="module-link-card"><b>${esc(m[2])}</b><span>Existing module screen. Keep its fields and permissions there.</span></div>`}
function configVaccinationPanel(){return `<div class="config-panel-title"><span class="eyebrow">Preventive Care</span><h3>Vaccination plan stays separate</h3><p>The plan has versions, dose rules, spacing and publish controls. It is not ported into this common setup page.</p></div><div class="module-link-card"><b>Vaccination plan</b><span>Redesign separately later.</span></div>`}
function entityRegisterInline(){return configWorkspaceInline();}
const entityInlineBaseRenderItems=renderItems;
renderItems=function(){entityInlineBaseRenderItems();const workspace=document.querySelector('.items-workspace');if(workspace&&!document.querySelector('#configuration-workspace'))workspace.insertAdjacentHTML('beforebegin',entityRegisterInline());document.body.classList.add('config-workspace-focused');};
if(typeof exposeGlobals==='function')exposeGlobals();
