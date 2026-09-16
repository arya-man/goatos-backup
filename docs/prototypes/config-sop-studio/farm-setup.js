/* Entity registers for review. No production API calls. */
const EntityRegistryModel={
 definitions:[
  {id:'parks',module:'Counts',name:'Parks',singular:'park',hint:'Top-level farm or site. Existing CBE and CPT are practice copies.',fields:['code','notes']},
  {id:'pens',module:'Counts',name:'Pens',singular:'pen',parent:'parks',hint:'Animal living area inside a park. Capacity is proposed; production currently has pen registration without capacity.',fields:['code','capacity','order','notes']},
  {id:'partitions',module:'Counts',name:'Pen partitions',singular:'partition',parent:'pens',hint:'A split inside a pen, used by feed, vaccination, weighing and task planning when work is done per partition.',fields:['code','capacity','order','notes']},
  {id:'species',module:'Counts',name:'Animal species',singular:'animal species',hint:'CRUD for the animal type itself: goat, sheep, cow, buffalo, prawn, fish, crab or any future species. Animals reference this record; it is not hardcoded.',fields:['code','notes']},
  {id:'breeds',module:'Counts',name:'Breed names',singular:'breed',parent:'species',hint:'Breed catalogue per species. SOPs and reports should reference these records instead of hardcoded breed text.',fields:['code','notes']},
  {id:'lifecycle_stages',module:'Counts',name:'Lifecycle stages',singular:'stage',parent:'species',hint:'Animal class definitions the user creates per species, such as K0, K1, K2, K3, adult female, buck, grow-out or any future livestock/aquaculture stage. Valuation, eligibility and lifecycle reports reference these records.',fields:['code','order','notes']},
  {id:'shed_tags',module:'Counts',name:'Shed tags / stage tags',singular:'shed tag',parent:'species',hint:'Operational housing/counting tags from the sheds setup. Goat/sheep examples can include K0/K1/K2/K3, ICU kids, warm-up, bucks and non-pregnant female groups; other species define their own tags. Feed rations, counts and SOP routing reference these records.',fields:['code','stage','notes']},
  {id:'animal_tags',module:'Counts',name:'Animal tags / groups',singular:'tag or group',parent:'species',hint:'Business cohort tags used for filtering and SOP routing, separate from RFID identity and shed tags.',fields:['code','notes']},
  {id:'animals',module:'Counts',name:'Animals',singular:'animal',parent:'pens',hint:'Animal identity stays in Counts Herd Register. Animals reference species, breed names, lifecycle stages and shed tags from the registers above.',fields:['tag1','tag2','species','sex','breed','stage','shedTag','weightKg','notes']},
  {id:'vendors',module:'Procurement',name:'Vendors / sellers',singular:'vendor',hint:'Sellers used in procurement source entry and purchase approvals.',fields:['code','phone','notes']},
  {id:'buyers',module:'Sales',name:'Buyers',singular:'buyer',hint:'Buyers used by sales loads, buyer analytics and sale eligibility configuration.',fields:['code','phone','notes']},
  {id:'trucks',module:'Procurement',name:'Trucks',singular:'truck',hint:'Vehicle records for transport SOPs, sanitation proof and travel checks.',fields:['code','capacity','notes']},
  {id:'feed_catalogue',module:'Feed',name:'Feed catalogue',singular:'feed item',hint:'Feed names, units and catalogue choices shared into procurement, feed and warm-up SOPs.',fields:['code','unit','notes']},
  {id:'feed_stock',module:'Feed',name:'Feed stock',singular:'stock lot',parent:'feed_catalogue',hint:'Inventory stock used when a truck carries feed and when warm-up blends familiar and farm feed.',fields:['code','quantityKg','notes']},
  {id:'vaccines',module:'Preventive Care',name:'Vaccines',singular:'vaccine',hint:'Vaccine catalogue identity. Dose timing and clinical rules remain in Vaccination plan.',fields:['code','notes']},
  {id:'vaccine_stock',module:'Preventive Care',name:'Vaccine stock',singular:'stock lot',parent:'vaccines',hint:'Vial or stock on hand: reference number, expiry and quantity. This does not replace the published vaccination plan.',fields:['code','quantity','notes']},
  {id:'symptoms',module:'Health',name:'Symptoms',singular:'symptom',hint:'Health question choices that can map into diseases and protocols.',fields:['code','notes']},
  {id:'diseases',module:'Health',name:'Diseases',singular:'disease',hint:'Disease catalogue used by health triage and protocol selection.',fields:['code','notes']},
  {id:'health_protocols',module:'Health',name:'Health protocols',singular:'protocol',parent:'diseases',hint:'Vet-approved instructions and approval paths. This is a reference, not a treatment prescription.',fields:['code','approvalRole','notes']},
  {id:'people',module:'Others',name:'People and roles',singular:'person / role',hint:'Operators, admins, directors and approval roles used by SOP assignment and approvals.',fields:['code','role','phone','notes']},
  {id:'approvals',module:'Approvals',name:'Approval policies',singular:'approval policy',hint:'Who must approve selection, transport, warm-up completion, sales exceptions or treatment decisions.',fields:['code','approvalRole','notes']}
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
   animals:[],vendors:[],buyers:[],trucks:[],feed_catalogue:[],feed_stock:[],vaccines:[],vaccine_stock:[],symptoms:[],diseases:[],health_protocols:[],people:[],approvals:[]
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
function entityRegister(module='Counts'){const d=entityRegistryData(),defs=EntityRegistryModel.definitions,modules=[...new Set(defs.map(x=>x.module))],current=modules.includes(module)?module:modules[0],selected=defs.filter(x=>x.module===current);modal('Business entity registers',`<div class="entity-register"><div class="entity-register-head"><div><p><b>Business records:</b> parks, pens, species, breeds, lifecycle stages, tags, animals, vendors, buyers, trucks, stock, people and approvals. SOPs reference these records.</p></div><div class="entity-register-totals"><b>${defs.length}</b><span>registers</span></div></div><div class="entity-register-shell"><nav class="entity-register-modules" aria-label="Register modules">${modules.map(m=>`<button class="${m===current?'active':''}" onclick="entityRegister('${m}')"><span>${esc(m)}</span><b>${defs.filter(x=>x.module===m).length}</b></button>`).join('')}</nav><section class="entity-register-panel"><div class="entity-register-panel-head"><span class="eyebrow">${esc(current)}</span><h3>${esc(current)} registers</h3><p>${esc(entityModuleSummary(current))}</p></div><div class="entity-register-rows">${selected.map(def=>entityRegisterRow(d,def)).join('')}</div></section></div></div>`,true)}
function entityModuleSummary(module){return ({Counts:'Farm structure, species, breed names, lifecycle stages, shed tags and animal identity records.',Procurement:'Seller, transport and source records used by purchase and transit SOPs.',Sales:'Buyer and selling policy records used by sales and weighing views.',Feed:'Feed catalogue and stock used by procurement, ration and warm-up work.',"Preventive Care":'Vaccine inventory identity while the vaccination plan keeps clinical timing rules.',Health:'Symptoms, diseases and approved protocols used by health SOPs.',Others:'People and roles used for assignment and operating responsibility.',Approvals:'Approval policies for selection, transport, sale exceptions and treatment decisions.'})[module]||'Feature-owned operational records.'}
function entityRegisterRow(data,def){const parent=def.parent?EntityRegistryModel.def(def.parent):null;return `<article class="entity-register-row" role="button" tabindex="0" onclick="entityRegisterList('${def.id}')" onkeydown="if(event.key==='Enter'||event.key===' '){event.preventDefault();entityRegisterList('${def.id}')}"><div><strong>${esc(def.name)}</strong><span>${esc(entityCounts(data,def))}${parent?' · inside '+esc(labelForRegister(parent).toLowerCase()):''}</span></div><p>${esc(def.hint)}</p></article>`}
function entityRegisterList(kind){const d=entityRegistryData(),def=EntityRegistryModel.def(kind),rows=EntityRegistryModel.rows(d,kind),parentDef=def.parent?EntityRegistryModel.def(def.parent):null;modal(def.name,`<div class="entity-register"><div class="actions"><button onclick="entityRegister()">All registers</button>${canEdit()?`<button class="primary" onclick="entityRegisterEdit('${kind}')">Add ${esc(def.singular)}</button>`:''}</div><div class="tablewrap"><table class="items-table"><thead><tr><th>Name</th>${parentDef?'<th>'+esc(labelForRegister(parentDef))+'</th>':''}<th>Details</th><th>Status</th><th></th></tr></thead><tbody>${rows.map(r=>`<tr><td><button class="item-name" onclick="entityRegisterEdit('${kind}','${r.id}')">${esc(r.name)}</button><div class="muted">${esc(r.code||'No code')}</div></td>${parentDef?`<td>${esc(entityName(d,def.parent,r.parentId))}</td>`:''}<td>${entityDetails(def,r)}</td><td><span class="badge ${r.active?'':'amber'}">${r.active?'Active':'Archived'}</span></td><td>${r.active&&canEdit()?`<button onclick="entityRegisterEdit('${kind}','${r.id}')">Edit</button> <button onclick="entityArchive('${kind}','${r.id}')">Archive</button>`:''}</td></tr>`).join('')||`<tr><td colspan="${parentDef?5:4}"><div class="empty">No ${esc(def.name.toLowerCase())} added in this browser yet.</div></td></tr>`}</tbody></table></div></div>`,true)}
function entityName(data,kind,id){return EntityRegistryModel.rows(data,kind).find(r=>r.id===id)?.name||'Not selected'}
function labelForRegister(def){return def.singular.replace(/^./,c=>c.toUpperCase())}
function entityReferenceKind(field){return ({species:'species',breed:'breeds',stage:'lifecycle_stages',shedTag:'shed_tags'}[field])}
function entityValueLabel(data,field,value){const kind=entityReferenceKind(field);return kind?entityName(data,kind,value):value}
function entityDetails(def,r){const d=entityRegistryData();return `<div class="muted">${(def.fields||[]).filter(f=>f!=='code').map(f=>`${esc(labelForField(f))}: ${esc(entityValueLabel(d,f,r[f])??'Not set')}`).join(' · ')||'No extra fields'}</div>`}
function entityRegisterEdit(kind,id){if(!canEdit())return toast('Editing is not available for this role.');const d=entityRegistryData(),def=EntityRegistryModel.def(kind),r=EntityRegistryModel.rows(d,kind).find(x=>x.id===id)||{},parentDef=def.parent?EntityRegistryModel.def(def.parent):null;modal((id?'Edit ':'Add ')+def.singular,`<form id="entityform" onsubmit="event.preventDefault();entityRegisterSave('${kind}','${id||''}')"><label class="field">Name<input id="entity-name" required value="${esc(r.name||'')}"></label><label class="field">Code<input id="entity-code" value="${esc(r.code||'')}"></label>${parentDef?`<label class="field">${esc(parentDef.singular.replace(/^./,c=>c.toUpperCase()))}<select id="entity-parent">${EntityRegistryModel.rows(d,def.parent).filter(p=>p.active).map(p=>`<option value="${p.id}" ${p.id===r.parentId?'selected':''}>${esc(p.name)}</option>`).join('')}</select></label>`:''}${(def.fields||[]).filter(f=>f!=='code').map(f=>entityField(f,r[f],kind)).join('')}<button type="button" onclick="entityRegisterList('${kind}')">Back</button> <button class="primary" type="submit">Save ${esc(def.singular)}</button></form>`,true)}
function entityField(f,value,kind){const numeric=['capacity','order','quantity','quantityKg','weightKg'].includes(f);const label=labelForField(f);const ref=entityReferenceKind(f);if(ref){const d=entityRegistryData(),rows=EntityRegistryModel.rows(d,ref).filter(r=>r.active);return `<label class="field">${label}<select id="entity-${f}"><option value="">Not selected</option>${rows.map(r=>`<option value="${esc(r.id)}" ${r.id===value?'selected':''}>${esc(r.name)}</option>`).join('')}</select></label>`}if(f==='notes')return `<label class="field">${label}<textarea id="entity-${f}">${esc(value||'')}</textarea></label>`;return `<label class="field">${label}<input id="entity-${f}" ${numeric?'type="number" min="0" step="0.01"':''} value="${esc(value??'')}"></label>`}
function entityRegisterSave(kind,id){if(!canEdit())return toast('Editing is not available for this role.');const def=EntityRegistryModel.def(kind),input={id,name:$('#entity-name').value,code:$('#entity-code').value,parentId:def.parent?$('#entity-parent').value:null};for(const f of def.fields||[])if(f!=='code')input[f]=$('#entity-'+f)?.value||'';try{EntityRegistryModel.save(entityRegistryData(),kind,input);persist();entityRegisterList(kind)}catch(e){toast(e.message)}}
function entityArchive(kind,id){if(!canEdit())return toast('Editing is not available for this role.');try{EntityRegistryModel.archive(entityRegistryData(),kind,id);persist();entityRegisterList(kind)}catch(e){toast(e.message)}}
function farmSetup(){entityRegisterList('parks')}
function farmSetupEdit(kind,id){entityRegisterEdit(kind==='park'?'parks':'pens',id)}
function farmSetupSave(kind,id){entityRegisterSave(kind==='park'?'parks':'pens',id)}
function farmSetupArchive(kind,id){entityArchive(kind==='park'?'parks':'pens',id)}
const farmSetupBaseItems=renderItems;renderItems=function(){farmSetupBaseItems();const target=$('.items-heading');if(target&&!$('#farm-setup-entry'))target.insertAdjacentHTML('beforeend','<button id="farm-setup-entry" onclick="entityRegister()">Entity registers</button>')};

function entityRegisterUses(def){
 const uses={
  parks:['Counts analytics','Weighing pen assignment','Feed and vaccination scope'],
  pens:['Weighing Session','Feed Distribution','Vaccination drives','Health tasks'],
  partitions:['Feed planning','Vaccination stock','Weighing by partition'],
  species:['Any species setup','Breed setup','Animal register'],
  breeds:['Animal register','Procurement source selection','Sales and valuation filters'],
  lifecycle_stages:['User-defined lifecycle classes','Sales valuation settings','Eligibility and lifecycle reports'],
  shed_tags:['Sheds setup','Counts breakdown','Feed ration rules','Weighing and vaccination scope'],
  animal_tags:['Breeding/fattening groups','Procurement routing','Sales and feed filters'],
  animals:['Herd Register','Procurement approved animals','Health and vaccination tasks'],
  vendors:['Procurement Source Entry','Animal Purchase Inspection','Feed Purchases'],
  buyers:['Sales loads','Buyer analytics','Sales Config'],
  trucks:['Transit checks','Truck sanitation proof','Arrival review'],
  feed_catalogue:['Feed SOP','Procurement transit feed','Warm-up feed transition'],
  feed_stock:['Feed inventory','Truck supplies','Warm-up ration planning'],
  vaccines:['Vaccination plan','Procurement holding','Preventive Care drives'],
  vaccine_stock:['Vaccination stock used','Expiry checks','Drive verification'],
  symptoms:['Health triage','Disease matching','Operator symptom capture'],
  diseases:['Health analytics','Protocol selection','Director approval'],
  health_protocols:['Health Config','Treatment SOP','Vet approval'],
  people:['Task assignment','Verifier handoff','Approval routing'],
  approvals:['CEO/CXO decisions','Director approvals','Exception handling'],
 };
 return uses[def.id]||['Feature SOPs'];
}
function entityRegisterInline(){
 const d=entityRegistryData(),defs=EntityRegistryModel.definitions;
 return `<section class="card entity-inline" id="entity-registers"><div class="cardheader"><div><span class="eyebrow">Business entity registers</span><h2>Business records used by SOPs and reports</h2></div></div><p class="muted">Set up parks, pens, species, breeds, lifecycle stages, tags, animals, vendors, buyers, trucks, stock and approval records once. Cards open the register.</p><div class="entity-inline-grid">${defs.map(def=>`<article class="entity-register-card" role="button" tabindex="0" onclick="entityRegisterList('${def.id}')" onkeydown="if(event.key==='Enter'||event.key===' '){event.preventDefault();entityRegisterList('${def.id}')}"><div><span class="eyebrow">${esc(def.module)}</span><h3>${esc(def.name)}</h3><p>${esc(entityCounts(d,def))}</p></div><small>${entityRegisterUses(def).map(esc).join(' · ')}</small></article>`).join('')}</div></section>`;
}
const entityInlineBaseRenderItems=renderItems;
renderItems=function(){entityInlineBaseRenderItems();const workspace=document.querySelector('.items-workspace');if(workspace&&!document.querySelector('#entity-registers'))workspace.insertAdjacentHTML('beforebegin',entityRegisterInline());};
if(typeof exposeGlobals==='function')exposeGlobals();
