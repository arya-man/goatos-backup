const fs=require('fs'),vm=require('vm'),assert=require('assert');
const context={state:{dataSources:[]},renderItems(){},commitItem(){},renderHierarchyEditor(){},itemModal(){},saveItem(){},availableItems(){return []},sourceOptions(){return []},itemVisibleConsumers(){return []}};vm.createContext(context);vm.runInContext(fs.readFileSync(__dirname+'/generic-items.js','utf8'),context);
const result=(item,sources=[])=>JSON.parse(JSON.stringify(context.genericItemAccess(item,sources)));
const needle={module:'Health',categoryId:'consumables',shares:['Preventive Care'],active:true};
assert.deepEqual(result(needle).map(x=>x.module),['Health','Preventive Care']);
assert(result(needle).every(x=>x.selectable));
const inherited=[{kind:'items',owner:'Health',categoryId:'consumables',shares:['Feed','Preventive Care']},{kind:'items',owner:'Procurement',categoryId:'other',shares:['Sales']}];
assert.deepEqual(result(needle,inherited).map(x=>x.module),['Health','Feed','Preventive Care']);
assert.equal(result(needle,inherited).find(x=>x.module==='Feed').reason,'Reusable list: undefined');
assert(result({...needle,active:false},inherited).every(x=>!x.selectable));
assert.deepEqual(result({...needle,shares:[]},inherited).map(x=>x.module),['Health','Feed','Preventive Care']);
console.log('PASS generic item preview: direct access, inherited source access, deduplication, isolation, archival');
// Run the actual hierarchy helpers and new inline creation handler together.
const elements={imodule:{value:'Health'},icategory:{value:'',innerHTML:''},isubcategory:{value:'',innerHTML:''},'generic-category-name':{value:'Consumables'},'generic-subcategory-name':{value:'Needles'},'generic-category-feedback':{textContent:''},iname:{value:'Needle'},ipurpose:{value:'Procedure supplies'},idescription:{value:'Keep this draft'}};
let editable=true,saved=0,seq=0;
const integrated={state:{items:[],itemCategories:[]},crypto:{randomUUID:()=>String(++seq)},canEdit:()=>editable,record:()=>saved++,esc:String,$:q=>elements[q.slice(1)]||null,document:{querySelectorAll:()=>[]},nodeCatalogue(){return []},render(){},inspect(){},validateWorkflow(){return []},compileWorkflow(){return {}},drawGraph(){},console};
vm.createContext(integrated);vm.runInContext(fs.readFileSync(__dirname+'/items.js','utf8'),integrated);vm.runInContext(fs.readFileSync(__dirname+'/shared-sources.js','utf8'),integrated);vm.runInContext(fs.readFileSync(__dirname+'/generic-items.js','utf8'),integrated);
integrated.genericCreateCategory(false);assert.equal(integrated.state.itemCategories[0].name,'Consumables');assert.equal(saved,1);assert.equal(elements.iname.value,'Needle');assert.equal(elements.ipurpose.value,'Procedure supplies');assert.equal(elements.idescription.value,'Keep this draft');
elements.icategory.value=integrated.state.itemCategories[0].id;integrated.genericCreateCategory(true);assert.equal(integrated.state.itemCategories[1].parentId,elements.icategory.value);assert.equal(saved,2);
elements['generic-subcategory-name'].value='needles';integrated.genericCreateCategory(true);assert.equal(integrated.state.itemCategories.length,2);assert.equal(saved,2);
elements['generic-category-name'].value='Blocked group';editable=false;integrated.genericCreateCategory(false);assert.equal(integrated.state.itemCategories.length,2);assert.equal(saved,2);
editable=true;elements.icategory.value='';elements['generic-subcategory-name'].value='Missing parent';integrated.genericCreateCategory(true);assert.equal(integrated.state.itemCategories.length,2);assert.equal(elements['generic-category-feedback'].textContent,'Choose or add a category first.');
console.log('PASS actual item hierarchy integration: draft preserved, stable parent IDs, duplicate reuse, readonly and missing-parent guards');

const generic={id:'needle-global',name:'Needle',module:'Common',categoryId:'generic-supplies',shares:['Health','Preventive Care'],active:true};
integrated.state.itemCategories.push({id:'generic-supplies',module:'Common',name:'Supplies',parentId:null});integrated.state.items.push(generic);integrated.ensureSources();const src=integrated.state.dataSources.find(s=>s.categoryId==='generic-supplies');src.shares=['Procurement'];
assert.deepEqual(result(generic,[src]).map(x=>x.module),['Procurement','Health','Preventive Care']);
for(const module of ['Health','Preventive Care','Procurement']){assert(integrated.availableItems(module).some(i=>i.id===generic.id));assert(integrated.sourceOptions(src,module).some(i=>i.id===generic.id));assert(integrated.nodeCatalogue({catalogueSourceId:src.id},module).some(i=>i.id===generic.id));}
for(const module of ['Feed','Common']){assert(!integrated.availableItems(module).some(i=>i.id===generic.id));assert(!integrated.sourceOptions(src,module).some(i=>i.id===generic.id));assert(!integrated.nodeCatalogue({catalogueSourceId:src.id},module).some(i=>i.id===generic.id));}
generic.active=false;assert(!integrated.availableItems('Health').some(i=>i.id===generic.id));assert(!integrated.sourceOptions(src,'Health').some(i=>i.id===generic.id));
console.log('PASS shared catalogue item reaches actual module/SOP selectors through direct and inherited source links; archive blocks new choices');
elements.imodule.value='Common';elements['generic-category-name'].value='Electrical appliances';integrated.genericCreateCategory(false);const globalCategory=integrated.state.itemCategories.find(c=>c.name==='Electrical appliances');assert.equal(globalCategory.module,'Common');elements.icategory.value=globalCategory.id;elements['generic-subcategory-name'].value='Fans';integrated.genericCreateCategory(true);assert(integrated.state.itemCategories.some(c=>c.name==='Fans'&&c.module==='Common'&&c.parentId===globalCategory.id));console.log('PASS arbitrary shared catalogue taxonomy has no consuming module owner');
const reopenChecks=[{dataset:{itemShare:'Counts'},checked:true},{dataset:{itemShare:'Health'},checked:true},{dataset:{itemShare:'Preventive Care'},checked:true},{dataset:{itemShare:'Procurement'},checked:false}];integrated.document.querySelectorAll=()=>reopenChecks;integrated.genericRestoreConsumerChecks({module:'Common',shares:['Health','Preventive Care']});assert.deepEqual(reopenChecks.filter(c=>c.checked).map(c=>c.dataset.itemShare),['Health','Preventive Care']);integrated.genericRestoreConsumerChecks(null);assert(reopenChecks.every(c=>!c.checked));console.log('PASS reopening shared catalogue item removes base-form phantom owner checkbox and preserves exact saved links');

generic.active=true;generic.shares=[];src.shares=[];const parent=integrated.itemCategory('generic-supplies');parent.shares=['Health'];integrated.state.itemCategories.push({id:'generic-sub',name:'Medical supplies',module:'Common',parentId:parent.id,shares:['Preventive Care']});generic.subcategoryId='generic-sub';
for(const module of ['Health','Preventive Care'])assert(integrated.nodeCatalogue({catalogueSourceId:src.id},module).some(i=>i.id===generic.id));
assert(integrated.genericItemAccess(generic).some(a=>a.module==='Health'&&a.reason.includes('Category: Supplies')));assert(integrated.genericItemAccess(generic).some(a=>a.module==='Preventive Care'&&a.reason.includes('Subcategory: Medical supplies')));
const snapshot=JSON.parse(JSON.stringify(generic));integrated.genericSetHierarchyLink(parent.id,'Health',false,true);assert(!integrated.availableItems('Health').some(i=>i.id===generic.id));assert(integrated.availableItems('Preventive Care').some(i=>i.id===generic.id));assert.equal(snapshot.subcategoryId,'generic-sub');
console.log('PASS category-only/subcategory-only actual SOP choices, granting-level provenance and inherited revoke');

const summary={textContent:''},openPanel={dataset:{hierarchyId:parent.id},open:true,querySelector:()=>summary};integrated.document.querySelectorAll=()=>[openPanel];integrated.genericSetHierarchyLink(parent.id,'Feed',true);assert.equal(openPanel.open,true);assert(summary.textContent.includes('departments (1)'));assert(parent.shares.includes('Feed'));console.log('PASS hierarchy checkbox preserves open panel and existing controls while updating count');

assert(integrated.itemMatchesModule(generic,'Preventive Care'));assert(integrated.itemMatchesModule(generic,'Common'));assert(!integrated.itemMatchesModule(generic,'Sales'));assert(integrated.itemMatchesModule(generic,''));assert(integrated.itemMatchesModule(generic,'Feed'));console.log('PASS business-module registry filter follows inherited effective grants; Shared catalogue retains origin semantics');
