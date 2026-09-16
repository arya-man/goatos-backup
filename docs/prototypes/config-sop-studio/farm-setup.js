/* Browser-only entity registers. No production API calls. */
const EntityRegistryModel={
 definitions:[
  {id:'parks',module:'Counts',name:'Parks',singular:'park',hint:'Top-level farm or site. Existing CBE and CPT are practice copies.',fields:['code','notes']},
  {id:'pens',module:'Counts',name:'Pens',singular:'pen',parent:'parks',hint:'Animal living area inside a park. Capacity is proposed; production currently has pen registration without capacity.',fields:['code','capacity','order','notes']},
  {id:'partitions',module:'Counts',name:'Pen partitions',singular:'partition',parent:'pens',hint:'A split inside a pen, used by feed, vaccination, weighing and task planning when work is done per partition.',fields:['code','capacity','order','notes']},
  {id:'animals',module:'Counts',name:'Animals',singular:'animal',parent:'pens',hint:'Animal identity stays in Counts Herd Register. This mock shows that animal CRUD belongs in the same register family.',fields:['tag1','tag2','species','sex','breed','stage','weightKg','notes']},
  {id:'vendors',module:'Procurement',name:'Vendors / sellers',singular:'vendor',hint:'Sellers used in procurement source entry and purchase approvals.',fields:['code','phone','notes']},
  {id:'buyers',module:'Sales',name:'Buyers',singular:'buyer',hint:'Buyers used by sales loads, buyer analytics and sale eligibility configuration.',fields:['code','phone','notes']},
  {id:'trucks',module:'Procurement',name:'Trucks',singular:'truck',hint:'Vehicle records for transport SOPs, sanitation proof and travel checks.',fields:['code','capacity','notes']},
  {id:'feed_catalogue',module:'Feed',name:'Feed catalogue',singular:'feed item',hint:'Feed names, units and catalogue choices shared into procurement, feed and warm-up SOPs.',fields:['code','unit','notes']},
  {id:'feed_stock',module:'Feed',name:'Feed stock lots',singular:'stock lot',parent:'feed_catalogue',hint:'Inventory batches used when a truck carries feed and when warm-up blends familiar and farm feed.',fields:['code','quantityKg','notes']},
  {id:'vaccines',module:'Preventive Care',name:'Vaccines',singular:'vaccine',hint:'Vaccine catalogue identity. Dose timing and clinical rules remain in Vaccination plan.',fields:['code','notes']},
  {id:'vaccine_batches',module:'Preventive Care',name:'Vaccine batches',singular:'batch',parent:'vaccines',hint:'Batch identity for stock and proof; does not replace the published vaccination plan.',fields:['code','quantity','notes']},
  {id:'symptoms',module:'Health',name:'Symptoms',singular:'symptom',hint:'Health question choices that can map into diseases and protocols.',fields:['code','notes']},
  {id:'diseases',module:'Health',name:'Diseases',singular:'disease',hint:'Disease catalogue used by health triage and protocol selection.',fields:['code','notes']},
  {id:'health_protocols',module:'Health',name:'Health protocols',singular:'protocol',parent:'diseases',hint:'Vet-approved instructions and approval paths. This is a reference, not a treatment prescription.',fields:['code','approvalRole','notes']},
  {id:'people',module:'Others',name:'People and roles',singular:'person / role',hint:'Operators, admins, directors and approval roles used by SOP assignment and approvals.',fields:['code','role','phone','notes']},
  {id:'approvals',module:'Approvals',name:'Approval policies',singular:'approval policy',hint:'Who must approve selection, transport, warm-up completion, sales exceptions or treatment decisions.',fields:['code','approvalRole','notes']},
  {id:'sop_templates',module:'Configuration',name:'SOP templates',singular:'SOP template',hint:'Reusable work instructions that can be stitched into a master SOP.',fields:['code','notes']}
 ],
 seed(){
  return {records:{
   parks:[{id:'practice-cbe',name:'CBE',code:'CBE',active:true},{id:'practice-cpt',name:'CPT',code:'CPT',active:true}],
   pens:[],partitions:[],animals:[],vendors:[],buyers:[],trucks:[],feed_catalogue:[],feed_stock:[],vaccines:[],vaccine_batches:[],symptoms:[],diseases:[],health_protocols:[],people:[],approvals:[],sop_templates:[]
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
function labelForField(f){return ({tag1:'Primary tag',tag2:'Second tag',weightKg:'Weight',quantityKg:'Quantity',approvalRole:'Approval role'}[f]||f.replace(/([A-Z])/g,' $1')).replace(/^./,c=>c.toUpperCase())}
function entityRegistryData(){if(!state.entityRegistry){state.entityRegistry=EntityRegistryModel.seed();if(state.farmSetup){state.entityRegistry.records.parks=state.farmSetup.parks||state.entityRegistry.records.parks;state.entityRegistry.records.pens=state.farmSetup.pens||[];}}return state.entityRegistry}
function farmSetupData(){const d=entityRegistryData();return {parks:d.records.parks,pens:d.records.pens,records:d.records}}
function entityCounts(d,def){const rows=EntityRegistryModel.rows(d,def.id),active=rows.filter(r=>r.active).length;return `${active} active${rows.length!==active?' · '+(rows.length-active)+' archived':''}`}
function entityRegister(module='Counts'){const d=entityRegistryData(),defs=EntityRegistryModel.definitions,modules=[...new Set(defs.map(x=>x.module))],current=modules.includes(module)?module:modules[0],selected=defs.filter(x=>x.module===current);modal('Entity registers',`<div class="entity-register"><div class="entity-register-head"><div><p>Set up the operational records each feature depends on. Browser-only practice registers; production records are not changed.</p></div><div class="entity-register-totals"><b>${defs.length}</b><span>registers</span></div></div><div class="entity-register-shell"><nav class="entity-register-modules" aria-label="Register modules">${modules.map(m=>`<button class="${m===current?'active':''}" onclick="entityRegister('${m}')"><span>${esc(m)}</span><b>${defs.filter(x=>x.module===m).length}</b></button>`).join('')}</nav><section class="entity-register-panel"><div class="entity-register-panel-head"><span class="eyebrow">${esc(current)}</span><h3>${esc(current)} registers</h3><p>${esc(entityModuleSummary(current))}</p></div><div class="entity-register-rows">${selected.map(def=>entityRegisterRow(d,def)).join('')}</div></section></div></div>`,true)}
function entityModuleSummary(module){return ({Counts:'Farm structure and animal identity: park, pen, partition and animal records.',Procurement:'Seller, transport and source records used by purchase and transit SOPs.',Sales:'Buyer and selling policy records used by sales and weighing views.',Feed:'Feed catalogue and stock lots used by procurement, ration and warm-up work.',"Preventive Care":'Vaccine inventory identity while the vaccination plan keeps clinical timing rules.',Health:'Symptoms, diseases and approved protocols used by health SOPs.',Others:'People and roles used for assignment and operating responsibility.',Approvals:'Approval policies for selection, transport, sale exceptions and treatment decisions.',Configuration:'Reusable SOP templates that can be stitched into master work instructions.'})[module]||'Feature-owned operational records.'}
function entityRegisterRow(data,def){const parent=def.parent?EntityRegistryModel.def(def.parent):null;return `<article class="entity-register-row"><div><strong>${esc(def.name)}</strong><span>${esc(entityCounts(data,def))}${parent?' · inside '+esc(labelForRegister(parent).toLowerCase()):''}</span></div><p>${esc(def.hint)}</p><button onclick="entityRegisterList('${def.id}')">Open</button></article>`}
function entityRegisterList(kind){const d=entityRegistryData(),def=EntityRegistryModel.def(kind),rows=EntityRegistryModel.rows(d,kind),parentDef=def.parent?EntityRegistryModel.def(def.parent):null;modal(def.name,`<div class="entity-register"><p>${esc(def.hint)}</p><div class="actions"><button onclick="entityRegister()">All registers</button>${canEdit()?`<button class="primary" onclick="entityRegisterEdit('${kind}')">Add ${esc(def.singular)}</button>`:''}</div><div class="tablewrap"><table class="items-table"><thead><tr><th>Name</th>${parentDef?'<th>'+esc(labelForRegister(parentDef))+'</th>':''}<th>Details</th><th>Status</th><th></th></tr></thead><tbody>${rows.map(r=>`<tr><td><button class="item-name" onclick="entityRegisterEdit('${kind}','${r.id}')">${esc(r.name)}</button><div class="muted">${esc(r.code||'No code')}</div></td>${parentDef?`<td>${esc(entityName(d,def.parent,r.parentId))}</td>`:''}<td>${entityDetails(def,r)}</td><td><span class="badge ${r.active?'':'amber'}">${r.active?'Active':'Archived'}</span></td><td>${r.active&&canEdit()?`<button onclick="entityRegisterEdit('${kind}','${r.id}')">Edit</button> <button onclick="entityArchive('${kind}','${r.id}')">Archive</button>`:''}</td></tr>`).join('')||`<tr><td colspan="${parentDef?5:4}"><div class="empty">No ${esc(def.name.toLowerCase())} added in this browser yet.</div></td></tr>`}</tbody></table></div></div>`,true)}
function entityName(data,kind,id){return EntityRegistryModel.rows(data,kind).find(r=>r.id===id)?.name||'Not selected'}
function labelForRegister(def){return def.singular.replace(/^./,c=>c.toUpperCase())}
function entityDetails(def,r){return `<div class="muted">${(def.fields||[]).filter(f=>f!=='code').map(f=>`${esc(labelForField(f))}: ${esc(r[f]??'Not set')}`).join(' · ')||'No extra fields'}</div>`}
function entityRegisterEdit(kind,id){if(!canEdit())return toast('Editing is not available for this role.');const d=entityRegistryData(),def=EntityRegistryModel.def(kind),r=EntityRegistryModel.rows(d,kind).find(x=>x.id===id)||{},parentDef=def.parent?EntityRegistryModel.def(def.parent):null;modal((id?'Edit ':'Add ')+def.singular,`<form id="entityform" onsubmit="event.preventDefault();entityRegisterSave('${kind}','${id||''}')"><label class="field">Name<input id="entity-name" required value="${esc(r.name||'')}"></label><label class="field">Code<input id="entity-code" value="${esc(r.code||'')}"></label>${parentDef?`<label class="field">${esc(parentDef.singular.replace(/^./,c=>c.toUpperCase()))}<select id="entity-parent">${EntityRegistryModel.rows(d,def.parent).filter(p=>p.active).map(p=>`<option value="${p.id}" ${p.id===r.parentId?'selected':''}>${esc(p.name)}</option>`).join('')}</select></label>`:''}${(def.fields||[]).filter(f=>f!=='code').map(f=>entityField(f,r[f])).join('')}<p class="muted">Saved in this browser only.</p><button type="button" onclick="entityRegisterList('${kind}')">Back</button> <button class="primary" type="submit">Save ${esc(def.singular)}</button></form>`,true)}
function entityField(f,value){const numeric=['capacity','order','quantity','quantityKg','weightKg'].includes(f);const label=labelForField(f);if(f==='notes')return `<label class="field">${label}<textarea id="entity-${f}">${esc(value||'')}</textarea></label>`;return `<label class="field">${label}<input id="entity-${f}" ${numeric?'type="number" min="0" step="0.01"':''} value="${esc(value??'')}"></label>`}
function entityRegisterSave(kind,id){if(!canEdit())return toast('Editing is not available for this role.');const def=EntityRegistryModel.def(kind),input={id,name:$('#entity-name').value,code:$('#entity-code').value,parentId:def.parent?$('#entity-parent').value:null};for(const f of def.fields||[])if(f!=='code')input[f]=$('#entity-'+f)?.value||'';try{EntityRegistryModel.save(entityRegistryData(),kind,input);persist();entityRegisterList(kind)}catch(e){toast(e.message)}}
function entityArchive(kind,id){if(!canEdit())return toast('Editing is not available for this role.');try{EntityRegistryModel.archive(entityRegistryData(),kind,id);persist();entityRegisterList(kind)}catch(e){toast(e.message)}}
function farmSetup(){entityRegisterList('parks')}
function farmSetupEdit(kind,id){entityRegisterEdit(kind==='park'?'parks':'pens',id)}
function farmSetupSave(kind,id){entityRegisterSave(kind==='park'?'parks':'pens',id)}
function farmSetupArchive(kind,id){entityArchive(kind==='park'?'parks':'pens',id)}
const farmSetupBaseItems=renderItems;renderItems=function(){farmSetupBaseItems();const target=$('.items-heading');if(target&&!$('#farm-setup-entry'))target.insertAdjacentHTML('beforeend','<button id="farm-setup-entry" onclick="entityRegister()">Entity registers</button>')};
