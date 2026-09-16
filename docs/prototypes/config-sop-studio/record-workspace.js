const RECORD_FEED_SOURCE = [{"key": "concentrate", "name": "Concentrate", "active": false}, {"key": "mesha_adult_concentrate_goat", "name": "Mesha Adult Concentrate Goat", "active": false}, {"key": "mesha_adult_concentrate_sheep", "name": "Mesha Adult Concentrate Sheep", "active": false}, {"key": "dry_masoor_bhusa", "name": "Dry Masoor Bhusa", "active": true}, {"key": "toor_dal_bhusa_pellet", "name": "Toor Dal Bhusa Pellet", "active": false}, {"key": "hybrid", "name": "Hybrid", "active": false}, {"key": "cofs", "name": "COFS", "active": false}, {"key": "hedge_lucerne", "name": "Hedge Lucerne", "active": false}, {"key": "dry_maize", "name": "Dry Maize", "active": false}, {"key": "baking_soda", "name": "Baking Soda", "active": false}, {"key": "rgs_concentrate", "name": "RGS Concentrate", "active": false}, {"key": "vijay_concentrate", "name": "Vijay Concentrate", "active": false}, {"key": "mesha_kids_goat_concentrate", "name": "Mesha Kids Goat Concentrate", "active": false}, {"key": "mesha_kids_sheep_concentrate", "name": "Mesha Kids Sheep Concentrate", "active": false}, {"key": "uht_milk", "name": "UHT Milk", "active": true}, {"key": "mesha_kids_concentrate", "name": "Mesha Kids Concentrate", "active": true}, {"key": "mesha_adult_concentrate", "name": "Mesha Adult Concentrate", "active": true}];
/* Business records presentation. Reuses entity, item and source identities. */
let recordSelection='entity:species',recordSubcategory='',recordSearch='',recordStatus='active';
const recordEntityNames={species:'Animal types',breeds:'Breeds',sex_values:'Gender / sex',lifecycle_stages:'Lifecycle stages',shed_tags:'Shed / stage tags',animal_tags:'Animal tags / groups',parks:'Parks',pens:'Pens',partitions:'Pen partitions',animals:'Animal identities'};
function ensureRecordWorkspace(){
 ensureTypedConfig();ensureLists();entityRegistryData();if(typeof ensureHealthCourseData==='function')ensureHealthCourseData();
 if(!state.dataLists.some(s=>s.id==='source-buyers')){state.dataLists.push({id:'source-buyers',name:'Buyers',owner:'Sales',kind:'records',shares:['Procurement'],revision:1,records:[]});persist();}
 if(!state.recordWorkspaceSeeded){
  const category=itemCategory('cat-feed-nutrition');if(category)category.name='Feed items';
  if(!itemCategory('sub-feed-imported'))state.itemCategories.push({id:'sub-feed-imported',name:'Feed catalogue',module:'Feed',parentId:'cat-feed-nutrition'});
  for(const source of RECORD_FEED_SOURCE){
   if(state.items.some(i=>i.feedItemKey===source.key))continue;
   state.items.push({id:'feed-source-'+source.key,name:source.name,feedItemKey:source.key,module:'Feed',categoryId:'cat-feed-nutrition',subcategoryId:'sub-feed-imported',unit:'kg',active:source.active,shares:['Procurement'],revision:1,purpose:'',description:'',energy:null,dryMatter:null,wastage:null,sourceRef:'research/feature-config-value-readback.txt:8-24'});
  }
  // Keep pre-existing illustrative item identities available to old SOP snapshots.
  // They are not presented as imported Feed Config data.
  for(const id of ['item-feed-pellets','item-feed-hay']){const item=itemById(id);if(item)item.workspaceFixture=true;}
  state.recordWorkspaceSeeded=true;persist();
 }
}
function recordCategories(){
 ensureRecordWorkspace();
 const entityOrder=['species','breeds','sex_values','lifecycle_stages','shed_tags','animal_tags','parks','pens','partitions','animals'];
 return [...entityOrder.filter(id=>EntityRegistryModel.definitions.some(d=>d.id===id)).map(id=>({id:'entity:'+id,name:recordEntityNames[id],kind:'entity',entity:id})),
 ...state.itemCategories.filter(c=>!c.parentId&&!c.archived&&!['category-shared-configuration','cat-pc-vaccines','health-source-medicines'].includes(c.id)&&(state.items.some(i=>i.categoryId===c.id&&i.itemKind!=='config_value')||c.recordCategory)).map(c=>({id:'item:'+c.id,name:c.name,kind:'item',category:c.id,categoryIds:c.id==='cat-health-medicines'?[c.id,'health-source-medicines']:[c.id]})),
 {id:'partners',name:'Business partners',kind:'partners'}];
}
function recordCategory(){return recordCategories().find(c=>c.id===recordSelection)||recordCategories()[0];}
function recordRows(category=recordCategory()){
 if(category.kind==='entity')return EntityRegistryModel.rows(entityRegistryData(),category.entity);
 if(category.kind==='partners')return ['source-vendors','source-buyers'].flatMap(id=>(listById(id)?.records||[]).map(r=>({...r,sourceId:id,subcategoryId:id})));
 return state.items.filter(i=>(category.categoryIds||[category.category]).includes(i.categoryId)&&i.itemKind!=='config_value'&&!i.workspaceFixture);
}
function recordSubcategories(category=recordCategory()){
 if(category.kind==='entity'){const def=EntityRegistryModel.def(category.entity);return def.parent?EntityRegistryModel.rows(entityRegistryData(),def.parent).map(r=>({id:r.id,name:r.name})):[];}
 if(category.kind==='partners')return [{id:'source-vendors',name:'Vendors'},{id:'source-buyers',name:'Buyers'}];
 return state.itemCategories.filter(c=>(category.categoryIds||[category.category]).includes(c.parentId)&&!c.archived);
}
function selectRecordCategory(id){recordSelection=id;recordSubcategory='';recordSearch='';recordStatus='active';renderItems();}
function recordSelectSubcategory(id){recordSubcategory=id;renderItems();}
function recordRowSubcategory(row,category=recordCategory()){const id=category.kind==='entity'?row.parentId:row.subcategoryId;return recordSubcategories(category).find(s=>s.id===id)?.name||'—';}
renderItems=function(){
 ensureRecordWorkspace();document.body.classList.remove('config-workspace-focused');
 const categories=recordCategories(),category=recordCategory();recordSelection=category.id;
 $('#main').innerHTML=`<section class="records-page"><div class="records-heading"><div><span class="eyebrow">Configuration</span><h1>Business records</h1><p>Animal types, farm places and reusable catalogues.</p></div><div class="actions"><button onclick="bulkAnimalSetup()" ${canEdit()?'':'disabled'}>Set up animals together</button><button onclick="recordCategoryDialog()" ${canEdit()?'':'disabled'}>+ Category</button><button onclick="recordManageCategories()" ${canEdit()?'':'disabled'}>Manage categories</button></div></div><div class="records-layout"><nav class="records-categories" aria-label="Record categories"><span class="records-label">Categories</span>${categories.map(c=>`<button onclick="selectRecordCategory('${c.id}')" class="${c.id===category.id?'active':''}" ${c.id===category.id?'aria-current="true"':''}><span>${esc(c.name)}</span><small>${recordRows(c).filter(r=>r.active).length}</small></button>`).join('')}</nav><section class="records-content ${recordSubcategories(category).length?'':'records-no-subcategory'}"><div class="records-title"><div><div class="records-crumb">Category</div><h2>${esc(category.name)}</h2></div><button class="primary" onclick="recordAdd()" ${canEdit()?'':'disabled'}>+ Add record</button></div><div class="records-toolbar"><label>Subcategory<select id="record-subcategory" onchange="recordSelectSubcategory(this.value)" ${recordSubcategories(category).length?'':'disabled'}><option value="">${recordSubcategories(category).length?'All subcategories':'No subcategory needed'}</option>${recordSubcategories(category).map(s=>`<option value="${esc(s.id)}" ${s.id===recordSubcategory?'selected':''}>${esc(s.name)}</option>`).join('')}</select></label><label class="records-search">Search<input id="records-search" placeholder="Search ${esc(category.name.toLowerCase())}" value="${esc(recordSearch)}" oninput="recordSearch=this.value;renderRecordRows()"></label><label>Status<select onchange="recordStatus=this.value;renderRecordRows()"><option value="active" ${recordStatus==='active'?'selected':''}>Active</option><option value="all" ${recordStatus==='all'?'selected':''}>All</option><option value="archived" ${recordStatus==='archived'?'selected':''}>Archived</option></select></label></div><div class="records-table-wrap"><table class="records-table"><thead><tr><th>Record</th><th>Subcategory</th><th>Status</th><th><span class="sr-only">Edit</span></th></tr></thead><tbody id="records-rows"></tbody></table></div><div class="records-footer">Saved in this browser · Local prototype</div></section></div></section>`;
 renderRecordRows();
};
function renderRecordRows(){const category=recordCategory(),rows=recordRows(category).filter(r=>(recordStatus==='all'||Boolean(r.active)===(recordStatus==='active'))&&(!recordSubcategory||(category.kind==='entity'?r.parentId:r.subcategoryId)===recordSubcategory)&&r.name.toLowerCase().includes(recordSearch.toLowerCase()));
 $('#records-rows').innerHTML=rows.map(r=>`<tr><td><button class="record-name" onclick="recordEdit('${r.id}','${r.sourceId||''}')">${esc(r.name)}</button>${r.sourceRef?'<small class="record-provenance">From existing configuration</small>':''}</td><td>${esc(recordRowSubcategory(r,category))}</td><td><span class="status-pill ${r.active?'active':'archived'}">${r.active?'Active':'Archived'}</span></td><td><button aria-label="Edit ${esc(r.name)}" onclick="recordEdit('${r.id}','${r.sourceId||''}')">Edit</button></td></tr>`).join('')||'<tr><td colspan="4"><div class="empty">No records here yet.</div></td></tr>';
}
function recordAdd(){const category=recordCategory();if(category.kind==='entity'){entityRegisterEdit(category.entity);if(recordSubcategory&&$('#entity-parent'))$('#entity-parent').value=recordSubcategory;recordStyleDialog();}else recordItemDialog('',category);}
function recordEdit(id,sourceId){const category=recordCategory();if(category.kind==='entity'){entityRegisterEdit(category.entity,id);recordStyleDialog();const r=EntityRegistryModel.rows(entityRegistryData(),category.entity).find(r=>r.id===id);if(r&&canEdit())document.querySelector('#overlay form').insertAdjacentHTML('beforeend',`<button type="button" class="record-archive" onclick="recordToggleEntity('${category.entity}','${id}')">${r.active?'Archive':'Restore'} record</button>`);}else recordItemDialog(id,category,sourceId);}
function recordStyleDialog(){document.querySelector('#overlay .modal')?.classList.add('record-drawer');const code=$('#entity-code');if(code)code.closest('label').hidden=true;}
entityRegisterList=function(kind){recordSelection='entity:'+kind;closeModal();renderItems();};
const recordLegacyItemModal=itemModal;
itemModal=function(id){const item=id?itemById(id):null;if(item?.itemKind==='config_value'){recordLegacyItemModal(id);return;}const category=item?recordCategories().find(c=>(c.categoryIds||[c.category]).includes(item.categoryId)):recordCategory();if(category?.kind==='entity'&&!id)return recordAdd();recordItemDialog(id,category?.kind==='item'?category:recordCategories().find(c=>c.kind==='item'));};
categoryModal=function(){recordCategoryDialog();};
function recordItemDialog(id,category=recordCategory(),sourceId=''){
 if(!category)return;
 const partner=category.kind==='partners',old=partner?(listById(sourceId)?.records||[]).find(r=>r.id===id):id?itemById(id):null;
 const subcategories=recordSubcategories(category),selected=old?.subcategoryId||sourceId||recordSubcategory||subcategories[0]?.id||'';
 const dis=canEdit()?'':'disabled';
 modal(id?'Edit record':'Add record',`<form id="record-form" onsubmit="event.preventDefault();recordSave('${id||''}','${category.id}','${sourceId}')"><div class="record-context">${esc(category.name)}</div><label class="field">Name<input id="record-name" required maxlength="120" value="${esc(old?.name||'')}" ${dis}></label><label class="field">Subcategory<select id="record-form-subcategory" onchange="if(document.getElementById('record-new-subcategory'))document.getElementById('record-new-subcategory').hidden=this.value!=='__new'" ${dis}>${subcategories.map(s=>`<option value="${esc(s.id)}" ${selected===s.id?'selected':''}>${esc(s.name)}</option>`).join('')}${partner?'':'<option value="__new">+ Create subcategory</option>'}</select></label>${partner?'':'<label class="field" id="record-new-subcategory" hidden>New subcategory name<input id="record-subcategory-name" maxlength="120"></label>'}${partner?`<label class="field">Contact<input id="record-contact" value="${esc(old?.contact||'')}" ${dis}></label>`:category.category==='cat-feed-nutrition'?`<div class="record-fields"><label class="field">Energy (kcal/kg)<input id="record-energy" type="number" min="0" step="any" value="${old?.energy??''}" ${dis}></label><label class="field">Dry matter factor<input id="record-dryMatter" type="number" min="0" max="1" step="any" value="${old?.dryMatter??''}" ${dis}></label><label class="field">Wastage factor<input id="record-wastage" type="number" min="0" max="1" step="any" value="${old?.wastage??''}" ${dis}></label></div>`:''}<label class="field">Notes<textarea id="record-notes" rows="3" ${dis}>${esc(old?.purpose||old?.notes||'')}</textarea></label><label class="field">Status<select id="record-active" ${dis}><option value="true" ${old?.active!==false?'selected':''}>Active</option><option value="false" ${old?.active===false?'selected':''}>Archived</option></select></label><p id="record-error" role="alert" class="error"></p><div class="record-form-footer"><button type="button" onclick="closeModal()">Cancel</button><button class="primary" type="submit" ${dis}>Save record</button></div></form>`);recordStyleDialog();
 if(!selected&&$('#record-new-subcategory'))$('#record-new-subcategory').hidden=false;
}
function recordSave(id,categoryId,sourceId){
 if(!canEdit())return;
 try{
 const category=recordCategories().find(c=>c.id===categoryId),partner=category.kind==='partners',name=$('#record-name').value.trim(),subId=$('#record-form-subcategory').value,notes=$('#record-notes').value.trim(),active=$('#record-active').value==='true';
 if(!name)throw Error('Enter a record name.');
 const old=partner?(listById(sourceId)?.records||[]).find(r=>r.id===id):id?itemById(id):null;
 if(partner){
  const source=listById(subId);if(!source||!['source-vendors','source-buyers'].includes(subId))throw Error('Choose Vendors or Buyers.');
  if(old&&sourceId!==subId)throw Error('Keep the existing partner type. Add a separate record for another role.');
  if(source.records.some(r=>r.id!==id&&r.name.toLowerCase()===name.toLowerCase()))throw Error('This partner already exists.');
  const next={...old,id:id||'partner-'+crypto.randomUUID(),name,notes,contact:$('#record-contact').value.trim(),active};
  if(old)Object.assign(old,next);else source.records.push(next);source.revision++;record('Saved partner: '+name);closeModal();renderItems();return;
 }
 let parent=itemCategory(category.category),sub=itemCategory(subId),newSub=null;if(sub&&(category.categoryIds||[category.category]).includes(sub.parentId))parent=itemCategory(sub.parentId);
 if(subId==='__new'){
  const subName=$('#record-subcategory-name').value.trim();if(!subName)throw Error('Enter a subcategory name.');
  sub=state.itemCategories.find(c=>c.parentId===parent.id&&c.name.toLowerCase()===subName.toLowerCase());
  if(!sub){newSub={id:'category-'+crypto.randomUUID(),name:subName,module:parent.module,parentId:parent.id};sub=newSub;}
 }
 if(!sub||sub.parentId!==parent.id)throw Error('Choose a subcategory.');
 if(state.items.some(i=>i.id!==id&&i.categoryId===parent.id&&i.subcategoryId===sub.id&&i.name.toLowerCase()===name.toLowerCase()))throw Error('This record already exists in this subcategory.');
 const next={...old,id:id||'item-'+crypto.randomUUID(),name,categoryId:parent.id,subcategoryId:sub.id,module:parent.module,shares:old?.shares||[],unit:old?.unit||(parent.module==='Feed'?'kg':'record'),purpose:notes,description:old?.description||'',active,revision:(old?.revision||0)+1};
 if(parent.id==='cat-feed-nutrition')for(const key of ['energy','dryMatter','wastage']){const raw=$('#record-'+key).value;next[key]=raw===''?null:Number(raw);if(next[key]!==null&&(!Number.isFinite(next[key])||next[key]<0||(key!=='energy'&&next[key]>1)))throw Error('Enter valid nutrition values, or leave them blank.');}
 const usages=old&&old.active&&!active?itemUsages(id):[];
 if(usages.length){modal('Archive record?',`<p>${esc(name)} is referenced by ${usages.length} SOP steps. Published versions keep their saved record.</p><div class="actions"><button onclick="closeModal()">Cancel</button><button id="record-confirm-archive" class="primary">Archive record</button></div>`);$('#record-confirm-archive').onclick=()=>{if(canEdit()){if(newSub)state.itemCategories.push(newSub);commitItem(next);}};return;}
 if(newSub)state.itemCategories.push(newSub);recordStatus=active?'active':'archived';recordSubcategory='';recordSearch='';commitItem(next);
 }catch(e){$('#record-error').textContent=e.message;}
}
function recordCategoryDialog(){if(!canEdit())return;modal('Create category',`<form onsubmit="event.preventDefault();recordCategorySave()"><label class="field">Category name<input id="record-category-name" required maxlength="120" placeholder="e.g. Equipment"></label><label class="field">First subcategory<input id="record-category-sub" required maxlength="120" placeholder="e.g. Measuring devices"></label><p id="record-category-error" class="error" role="alert"></p><div class="record-form-footer"><button type="button" onclick="closeModal()">Cancel</button><button class="primary" type="submit">Create category</button></div></form>`);recordStyleDialog();}
function recordCategorySave(){if(!canEdit())return;const name=$('#record-category-name').value.trim(),sub=$('#record-category-sub').value.trim();if(!name||!sub)return;if(modules.some(m=>m.toLowerCase()===name.toLowerCase())){$('#record-category-error').textContent='Use a business record family, such as Medicines or Equipment.';return;}if(recordCategories().some(c=>c.name.toLowerCase()===name.toLowerCase())){$('#record-category-error').textContent='This category already exists.';return;}const id='category-'+crypto.randomUUID();state.itemCategories.push({id,name,module:'Common',parentId:null,recordCategory:true},{id:'category-'+crypto.randomUUID(),name:sub,module:'Common',parentId:id});recordSelection='item:'+id;recordSubcategory='';recordSearch='';recordStatus='active';record('Created category: '+name);closeModal();renderItems();}

function recordToggleEntity(kind,id){
 if(!canEdit())return;
 try{const data=structuredClone(entityRegistryData()),row=EntityRegistryModel.rows(data,kind).find(r=>r.id===id);if(!row)return;
 if(row.active)EntityRegistryModel.archive(data,kind,id);else{EntityRegistryModel.save(data,kind,{...row});row.active=true;}
 state.entityRegistry=data;record('Updated '+EntityRegistryModel.def(kind).singular+': '+row.name);closeModal();renderItems();
 }catch(e){toast(e.message);}
}
function recordManageCategories(){
 if(!canEdit())return;
 const categories=state.itemCategories.filter(c=>!c.parentId&&c.id!=='category-shared-configuration');
 modal('Manage categories',`<div class="record-category-management">${categories.map(c=>`<section><div class="record-manage-row"><label>Category<input id="record-rename-${c.id}" value="${esc(c.name)}"></label><button onclick="recordRenameCategory('${c.id}')">Save</button><button onclick="recordArchiveCategory('${c.id}')">${c.archived?'Restore':'Archive'}</button></div>${state.itemCategories.filter(sub=>sub.parentId===c.id).map(sub=>`<div class="record-manage-row sub"><label>Subcategory<input id="record-rename-${sub.id}" value="${esc(sub.name)}"></label><button onclick="recordRenameCategory('${sub.id}')">Save</button><button onclick="recordArchiveCategory('${sub.id}')">${sub.archived?'Restore':'Archive'}</button></div>`).join('')}<div class="record-manage-row sub"><label>New subcategory<input id="record-add-sub-${c.id}"></label><button onclick="recordAddSubcategory('${c.id}')" ${c.archived?'disabled':''}>Add</button></div></section>`).join('')}</div><p id="record-manage-error" class="error" role="alert"></p><button onclick="closeModal();renderItems()">Done</button>`,true);
}
function recordRenameCategory(id){if(!canEdit())return;const c=itemCategory(id),name=$('#record-rename-'+id).value.trim();if(!name)return;if(!c.parentId&&modules.some(m=>m.toLowerCase()===name.toLowerCase())){$('#record-manage-error').textContent='Use a business record family, not a module name.';return;}if(state.itemCategories.some(x=>x.id!==id&&(x.parentId||null)===(c.parentId||null)&&x.name.toLowerCase()===name.toLowerCase())){$('#record-manage-error').textContent='This name already exists.';return;}c.name=name;record('Renamed category: '+name);ensureLists();recordManageCategories();}
function recordArchiveCategory(id){if(!canEdit())return;const c=itemCategory(id),categoryIds=id==='cat-health-medicines'?['cat-health-medicines','health-source-medicines']:[id];if(!c.archived&&state.items.some(i=>i.active&&(categoryIds.includes(i.categoryId)||i.subcategoryId===id))){$('#record-manage-error').textContent='Archive the active records in this category first.';return;}if(c.archived&&c.parentId&&itemCategory(c.parentId)?.archived){$('#record-manage-error').textContent='Restore the parent category first.';return;}c.archived=!c.archived;record((c.archived?'Archived':'Restored')+' category: '+c.name);recordManageCategories();}
function recordAddSubcategory(id){if(!canEdit())return;const parent=itemCategory(id),name=$('#record-add-sub-'+id).value.trim();if(!name)return;if(state.itemCategories.some(c=>c.parentId===id&&c.name.toLowerCase()===name.toLowerCase())){$('#record-manage-error').textContent='This subcategory already exists.';return;}state.itemCategories.push({id:'category-'+crypto.randomUUID(),name,parentId:id,module:parent.module});record('Added subcategory: '+name);recordManageCategories();}
