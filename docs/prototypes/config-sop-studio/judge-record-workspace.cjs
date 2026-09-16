const fs=require('fs'),vm=require('vm'),assert=require('assert/strict');
const elements=new Map();
function element(selector){if(!elements.has(selector))elements.set(selector,{closest(){return this},options:[],value:selector==='#role'?'CEO / CXO':'',innerHTML:'',textContent:'',style:{},classList:{toggle(){},add(){},remove(){}},addEventListener(){},insertAdjacentHTML(where,html){this.innerHTML+=html},querySelector(s){return s.includes('Action type')?element(s):null},querySelectorAll(){return[]},prepend(e){this.innerHTML=e.innerHTML+this.innerHTML},append(e){this.innerHTML+=e.innerHTML},getBoundingClientRect(){return {width:1200,height:800,left:0,top:0}}});return elements.get(selector)}
const context={console,structuredClone,setTimeout:()=>0,clearTimeout(){},crypto:require('crypto').webcrypto,localStorage:{getItem(){return null},setItem(){}},document:{createElement:()=>({innerHTML:'',className:''}),querySelector:element,querySelectorAll(){return[]},body:element('body'),addEventListener(){}},window:{addEventListener(){}},location:{},requestAnimationFrame(){}};

vm.createContext(context);
for(const file of ['app.js','enhancements.js','items.js','studio-v2.js','health-course.js','shared-sources.js','generic-items.js','typed-config.js','farm-setup.js','record-workspace.js','record-category-manager.js'])vm.runInContext(fs.readFileSync(__dirname+'/'+file,'utf8'),context);
function run(code){return vm.runInContext(code,context)}
run('ensureRecordWorkspace()');
assert.equal(run('recordRows({kind:"item",category:"cat-feed-nutrition"}).length'),17);
assert.equal(run('recordRows({kind:"item",category:"cat-feed-nutrition"}).filter(r=>r.active).length'),4);
assert.equal(run('recordRows({kind:"item",category:"cat-feed-nutrition"}).every(r=>r.energy===null&&r.dryMatter===null&&r.wastage===null)'),true);
for(const kind of ['species','breeds','sex_values','lifecycle_stages','shed_tags','animal_tags','parks','pens','partitions','animals'])assert.equal(run(`recordCategories().some(c=>c.id==='entity:${kind}')`),true);
function fill(values){for(const [id,value] of Object.entries(values))element('#'+id).value=value;}
fill({'record-category-name':'Review instruments','record-category-sub':'Scales'});run('recordCategorySave();custom=recordCategory();recordItemDialog("",custom)');
assert.equal(run('itemCategory(custom.category).module'),'Common');
assert.doesNotMatch(element('#overlay').innerHTML,/id="imodule"|data-item-share|Used by modules/);
fill({'record-name':'Platform scale','record-form-subcategory':'__new','record-subcategory-name':'Portable scales','record-notes':'Keep this draft','record-active':'true'});
run('recordSave("",custom.id,"")');
assert.equal(element('#record-error').textContent,'');
assert.equal(run('recordRows(custom).length'),1);
assert.equal(run('recordRows(custom)[0].purpose'),'Keep this draft');
assert.equal(run('recordRowSubcategory(recordRows(custom)[0],custom)'),'Portable scales');
run('savedRecord=recordRows(custom)[0];savedId=savedRecord.id;savedSub=savedRecord.subcategoryId;recordItemDialog(savedId,custom)');
assert.match(element('#overlay').innerHTML,/Platform scale/);
fill({'record-name':'Platform scale revised','record-form-subcategory':run('savedSub'),'record-notes':'Edited','record-active':'true'});
run('recordSave(savedId,custom.id,"")');
assert.equal(run('recordRows(custom).length'),1);assert.equal(run('recordRows(custom)[0].id'),run('savedId'));assert.equal(run('recordRows(custom)[0].revision'),2);
// Invalid inline subcategory must leave both the source and the entered draft intact.
fill({'record-name':'Second scale','record-form-subcategory':'__new','record-subcategory-name':'','record-notes':'Draft survives','record-active':'true'});
run('countBefore=state.itemCategories.length;recordSave("",custom.id,"")');
assert.match(element('#record-error').textContent,/subcategory name/);assert.equal(run('state.itemCategories.length'),run('countBefore'));assert.equal(element('#record-name').value,'Second scale');assert.equal(element('#record-notes').value,'Draft survives');
fill({'record-name':'Dry Masoor Bhusa updated','record-form-subcategory':'sub-feed-imported','record-notes':'','record-active':'true','record-energy':'','record-dryMatter':'','record-wastage':''});
run('recordSave("feed-source-dry_masoor_bhusa","item:cat-feed-nutrition","")');
assert.equal(run('itemById("feed-source-dry_masoor_bhusa").energy'),null);assert.equal(run('itemById("feed-source-dry_masoor_bhusa").feedItemKey'),'dry_masoor_bhusa');
fill({'record-name':'Supplier revised','record-form-subcategory':'source-vendors','record-notes':'Same source','record-contact':'contact','record-active':'true'});
run('vendor=listById("source-vendors").records[0];vendorId=vendor.id;recordSave(vendorId,"partners","source-vendors")');
assert.equal(run('vendor.name'),'Supplier revised');assert.equal(run('listById("source-vendors").records[0]===vendor'),true);assert.equal(run('recordRows({kind:"partners"}).find(r=>r.id===vendorId).name'),'Supplier revised');
fill({'record-name':'Buyer example','record-form-subcategory':'source-buyers','record-notes':'Buyer note','record-contact':'buyer contact','record-active':'true'});run('recordSave("","partners","")');
assert.equal(run('listById("source-buyers").records.length'),1);assert.equal(run('recordRows({kind:"partners"}).find(r=>r.sourceId==="source-buyers").id'),run('listById("source-buyers").records[0].id'));
run('state=JSON.parse(JSON.stringify(state));ensureRecordWorkspace()');
assert.equal(run('recordRows({kind:"item",category:"cat-feed-nutrition"}).length'),17);assert.equal(run('itemById(savedId).name'),'Platform scale revised');
console.log('PASS final record workspace: imported feed counts/null factors, entity categories, module-free custom records, inline subcategories/draft retention, stable edits, shared partner sources and reload');
// Exercise every entity family through the production model, then archive in child-first order.
run(`entityProof=EntityRegistryModel.seed();createdEntities={};for(const def of EntityRegistryModel.definitions){
 const parentId=def.parent?(def.parent==='pens'?createdEntities.pens.id:EntityRegistryModel.rows(entityProof,def.parent).find(r=>r.active).id):null;
 const row=EntityRegistryModel.save(entityProof,def.id,{name:'Review '+def.id,parentId});createdEntities[def.id]=row;
 const originalId=row.id;EntityRegistryModel.save(entityProof,def.id,{id:row.id,name:'Edited '+def.id,parentId});
 if(row.id!==originalId||row.name!=='Edited '+def.id)throw Error('Unstable edit: '+def.id);
 if(!EntityRegistryModel.rows(entityProof,def.id).includes(row))throw Error('Missing list row: '+def.id);
}
for(const kind of ['animals','partitions','pens','parks','breeds','lifecycle_stages','shed_tags','animal_tags','sex_values','species'])EntityRegistryModel.archive(entityProof,kind,createdEntities[kind].id);
`);
assert.equal(run('Object.values(createdEntities).every(r=>!r.active)'),true);
run('atomicOriginal=JSON.stringify(entityProof)');
assert.throws(()=>run('applyBulkAnimalSetup(entityProof,{species:"Review new species",breeds:"invalid line"})'),/format/);
assert.equal(run('JSON.stringify(entityProof)'),run('atomicOriginal'));
run('atomicResult=applyBulkAnimalSetup(entityProof,{species:"Review new species",breeds:"Review new species / Review breed",animal_tags:"Review new species / Review group"});atomicAgain=applyBulkAnimalSetup(atomicResult,{species:"Review new species",breeds:"Review new species / Review breed",animal_tags:"Review new species / Review group"})');
assert.equal(run('JSON.stringify(atomicResult)'),run('JSON.stringify(atomicAgain)'));
fill({'entity-parent':'__new__','entity-parent-name':'Draft pen','entity-parent-parent':'__new__','entity-parent-parent-name':'Draft park','entity-name':'Draft partition'});
run('parentDraft=EntityRegistryModel.seed();parentPen=entityResolveParent(parentDraft,"pens")');
assert.equal(run('parentDraft.records.pens.find(r=>r.id===parentPen).name'),'Draft pen');assert.equal(element('#entity-name').value,'Draft partition');
console.log('PASS all ten entity families create/edit/list/archive; invalid bulk rollback, repeat bulk idempotency and nested parent creation preserve draft');
fill({['record-rename-'+run('custom.category')]:'Review instruments renamed',['record-rename-'+run('savedSub')]:'Portable scales renamed'});
run('recordRenameCategory(custom.category);recordRenameCategory(savedSub)');
assert.equal(run('itemById(savedId).categoryId'),run('custom.category'));assert.equal(run('itemById(savedId).subcategoryId'),run('savedSub'));assert.equal(run('itemCategory(savedSub).name'),'Portable scales renamed');
run('recordArchiveCategory(savedSub)');assert.equal(run('Boolean(itemCategory(savedSub).archived)'),false);assert.match(element('#record-manage-error').textContent,/active records/);
run('recordArchiveCategory(custom.category)');assert.equal(run('Boolean(itemCategory(custom.category).archived)'),false);
fill({['record-add-sub-'+run('custom.category')]:'Unassigned instruments'});run('recordAddSubcategory(custom.category);addedSub=state.itemCategories.find(c=>c.name==="Unassigned instruments");recordArchiveCategory(addedSub.id)');assert.equal(run('addedSub.archived'),true);assert.equal(run('recordSubcategories(custom).some(s=>s.id===addedSub.id)'),false);
run('recordArchiveCategory(addedSub.id)');assert.equal(run('addedSub.archived'),false);
run('itemById(savedId).active=false;recordArchiveCategory(savedSub);recordArchiveCategory(custom.category)');assert.equal(run('itemCategory(custom.category).archived'),true);assert.equal(run('recordCategories().some(c=>c.id===custom.id)'),false);
run('recordArchiveCategory(savedSub)');assert.equal(run('itemCategory(savedSub).archived'),true);assert.match(element('#record-manage-error').textContent,/parent category/);
run('recordArchiveCategory(custom.category);recordArchiveCategory(savedSub)');assert.equal(run('itemCategory(custom.category).archived'),false);assert.equal(run('itemCategory(savedSub).archived'),false);
run('restoreEntity=EntityRegistryModel.save(entityRegistryData(),"species",{name:"Restore species"});recordToggleEntity("species",restoreEntity.id)');assert.equal(run('EntityRegistryModel.rows(entityRegistryData(),"species").find(r=>r.id===restoreEntity.id).active'),false);
run('recordToggleEntity("species",restoreEntity.id)');assert.equal(run('EntityRegistryModel.rows(entityRegistryData(),"species").find(r=>r.id===restoreEntity.id).active'),true);
console.log('PASS category/subcategory stable rename, active-reference archive guards, archive/restore visibility, parent restore ordering and entity restore');
fill({'record-name':'Buyer example revised','record-form-subcategory':'source-buyers','record-notes':'Archived buyer','record-contact':'buyer contact','record-active':'false'});run('buyerId=listById("source-buyers").records[0].id;recordSave(buyerId,"partners","source-buyers")');assert.equal(run('listById("source-buyers").records[0].id'),run('buyerId'));assert.equal(run('listById("source-buyers").records[0].active'),false);
run('state.dataLists=state.dataLists.filter(s=>s.id!=="source-buyers");state.dataSources=state.dataLists;state.recordWorkspaceSeeded=true;ensureRecordWorkspace()');assert.equal(run('state.dataLists.some(s=>s.id==="source-buyers"&&Array.isArray(s.records))'),true);
element('#role').value='Operator';run('readonlyBefore=JSON.stringify(state);recordCategorySave();recordSave("",custom.id,"");recordRenameCategory(custom.category);recordArchiveCategory(custom.category);recordAddSubcategory(custom.category);recordToggleEntity("species",restoreEntity.id)');assert.equal(run('JSON.stringify(state)'),run('readonlyBefore'));
console.log('PASS partner stable edit/archive, existing-browser buyer-list migration and read-only mutation guards');
element('#role').value='CEO / CXO';run('recordItemDialog("",{id:"partners",name:"Business partners",kind:"partners"})');
const partnerSelect=element('#overlay').innerHTML.match(/<select id="record-form-subcategory"[^>]*>/)[0],partnerChange=partnerSelect.match(/onchange="([^"]*)"/);
if(partnerChange){const check={document:{getElementById:()=>null},value:'source-buyers'};vm.runInNewContext('(function(){'+partnerChange[1]+'}).call({value:"source-buyers"})',check);}
console.log('PASS changing partner type tolerates absent inline subcategory field');

run('state.items.filter(i=>i.categoryId==="cat-health-medicines").forEach(i=>i.active=false);state.items.push({id:"review-imported-medicine",name:"Imported medicine",categoryId:"health-source-medicines",active:true});medicineBefore=JSON.stringify(state.items);recordArchiveCategory("cat-health-medicines")');assert.equal(run('Boolean(itemCategory("cat-health-medicines").archived)'),false);assert.match(element('#record-manage-error').textContent,/active records/);assert.equal(run('JSON.stringify(state.items)'),run('medicineBefore'));assert.equal(run('recordCategories().some(c=>c.category==="cat-health-medicines")'),true);
console.log('PASS merged medicine category archive guard protects active imported health records');

run('if(!itemCategory("health-source-medicines"))state.itemCategories.push({id:"health-source-medicines",name:"Imported Medicines",module:"Health",parentId:null});state.itemCategories.push({id:"review-imported-sub",name:"Imported treatments",module:"Health",parentId:"health-source-medicines"});recordManagedCategory="cat-health-medicines";recordManageCategories()');assert.equal(run('recordManagedCategories().some(c=>c.id==="health-source-medicines")'),false);assert.match(element('#main').innerHTML,/Imported treatments/);assert.doesNotMatch(element('#main').innerHTML,/recordChooseManagedCategory\('health-source-medicines'\)/);fill({'record-rename-review-imported-sub':'Imported treatments renamed'});run('recordRenameCategory("review-imported-sub")');assert.equal(run('itemCategory("review-imported-sub").parentId'),'health-source-medicines');assert.equal(run('itemCategory("review-imported-sub").name'),'Imported treatments renamed');run('itemById("review-imported-medicine").subcategoryId="review-imported-sub";recordArchiveCategory("review-imported-sub")');assert.equal(run('Boolean(itemCategory("review-imported-sub").archived)'),false);assert.match(element('#record-manage-error').textContent,/active records/);
console.log('PASS one Medicines manager category retains editable aliased subcategories and reference guards without changing source IDs');
