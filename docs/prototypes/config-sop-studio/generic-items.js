/* Item-first presentation. Existing stable IDs, access rules and save impact checks remain authoritative. */
function genericItemAccess(item, sources=state.dataSources||[]) {
  const grants=new Map(),add=(module,reason)=>{if(!module||module==='Common')return;grants.set(module,[...(grants.get(module)||[]),reason]);};
  if(item.module!=='Common')add(item.module,'Owning business area');
  const category=(state.itemCategories||[]).find(c=>c.id===item.categoryId),sub=(state.itemCategories||[]).find(c=>c.id===item.subcategoryId);
  (category?.shares||[]).forEach(m=>add(m,'Category: '+category.name));
  (sub?.shares||[]).forEach(m=>add(m,'Subcategory: '+sub.name));
  sources.filter(s=>s.kind==='items'&&s.owner===item.module&&s.categoryId===item.categoryId).forEach(s=>(s.shares||[]).forEach(m=>add(m,'Reusable list: '+s.name)));
  (item.shares||[]).forEach(m=>add(m,'Selected here'));
  return [...grants].map(([module,reasons])=>({module,selectable:!!item.active,reason:[...new Set(reasons)].join(' · ')}));
}
const genericBaseItems=renderItems;
renderItems=function(){genericBaseItems();const heading=$('.items-heading p');if(heading)heading.textContent='Add an item or setting once. Choose which departments can use it in their work instructions.';};
const genericBaseItemModal=itemModal;
itemModal=function(id){
  genericBaseItemModal(id);const form=$('#itemform');if(!form)return;
  const existing=id?itemById(id):null,isGeneric=!existing||existing.module==='Common';
  if(isGeneric){
    const moduleSelect=$('#imodule');moduleSelect.insertAdjacentHTML('afterbegin','<option value="Common">Reusable item library</option>');moduleSelect.value='Common';moduleSelect.disabled=true;
    genericRestoreConsumerChecks(existing);
    refreshItemCategories(existing?.categoryId||'',existing?.subcategoryId||'');
  }
  $('.item-drawer-intro').innerHTML=(id?'<span class="muted">Saved item · Revision '+itemById(id).revision+'</span>':'Create one reusable item or setting');
  const name=$('#iname').closest('label'),owner=$('#imodule').closest('label'),unit=$('#iunit').closest('label'),ownerRow=owner.parentElement;
  const shares=$('#itemshares');
  // Replace static sharing copy with the live effective-access preview.
  [...form.children].filter(el=>(el.tagName==='SPAN'&&el.textContent==='Share with other departments')||(el.tagName==='P'&&el.textContent.startsWith('The owning business area'))||(el.classList.contains('notice')&&el.textContent.startsWith('Additional access through shared sources:'))).forEach(el=>el.remove());
  const access=document.createElement('section');access.className='generic-access';access.innerHTML='<h3>Where can this item be used?</h3><p class="muted">'+(isGeneric?'Choose the departments that need this item or setting. Each selected team can use it in their work instructions.':'Tick the departments whose SOPs should be able to choose this item.')+'</p>';access.appendChild(shares);name.after(access);
  const basics=document.createElement('div');basics.className='inputrow';basics.appendChild(name);basics.appendChild(unit);form.prepend(basics);
  owner.querySelector('span').textContent=isGeneric?'Catalogue':'Owned by';ownerRow.classList.add('generic-maintenance');

  access.insertAdjacentHTML('beforeend','<div id="generic-access-preview" aria-live="polite"></div>');
  form.addEventListener('input',genericRefreshPreview);form.addEventListener('change',genericRefreshPreview);
  genericRefreshPreview();
};
function genericRefreshPreview(){
 const form=$('#itemform');if(!form)return;const id=(form.getAttribute('onsubmit')||'').match(/saveItem\('([^']*)'/)?.[1]||'';
 const old=id?itemById(id):null;
 const draft={id:id||'new',name:$('#iname').value.trim()||'This item',module:$('#imodule').value,categoryId:$('#icategory').value,subcategoryId:$('#isubcategory').value,active:$('#iactive').value==='true',shares:[...document.querySelectorAll('[data-item-share]:checked')].map(el=>el.dataset.itemShare)};
 const box=$('#generic-access-preview');if(!box)return;
 const access=genericItemAccess(draft);
 box.innerHTML='<h4>Where this can be used</h4><p class="muted">Select the business areas that can use it.</p><div class="item-access-pills">'+access.map(a=>'<span class="badge '+(a.reason==='Owning business area'?'':'blue')+'">'+esc(a.module)+(a.reason==='Owning business area'?' · owner':'')+'</span>').join('')+'</div>'+(draft.active?'':'<p class="muted">Archived items stay in old published SOPs but cannot be chosen for new work.</p>');
}
function genericCreateCategory(isSub){
 if(!canEdit())return;const input=$(isSub?'#generic-subcategory-name':'#generic-category-name'),feedback=$('#generic-category-feedback'),name=input.value.trim(),module=$('#imodule').value,parentId=isSub?$('#icategory').value:null;
 if(!name){feedback.textContent='Enter a name for the new group.';return;}if(isSub&&!parentId){feedback.textContent='Choose or add a category first.';return;}
 let group=state.itemCategories.find(c=>c.module===module&&(c.parentId||null)===parentId&&c.name.toLowerCase()===name.toLowerCase());
 if(!group){group={id:'category-'+crypto.randomUUID(),name,module,parentId};state.itemCategories.push(group);record('Added item '+(isSub?'subcategory: ':'category: ')+name);}
 if(isSub)refreshItemSubcategories(group.id);else refreshItemCategories(group.id);
 input.value='';feedback.textContent=group.name+' selected. Your item draft is preserved.';genericRefreshPreview();
}

const genericBaseSaveItem=saveItem;
saveItem=function(id){
 if(!canEdit())return;
 const draft={module:$('#imodule').value,categoryId:$('#icategory').value,subcategoryId:$('#isubcategory').value,active:$('#iactive').value==='true',shares:[...document.querySelectorAll('[data-item-share]:checked')].map(e=>e.dataset.itemShare)};
 if((!itemById(id)||draft.active)&&!genericItemAccess(draft).length){$('#itemerror').textContent='Link at least one department on this item, its category or subcategory.';return;}
 genericBaseSaveItem(id);
};

// One resolver powers item lists, source-backed questions, action pickers and validation.
availableItems=function(module){ensureSources();return state.items.filter(i=>i.active&&genericItemAccess(i).some(a=>a.module===module));};
const genericBaseSourceOptions=sourceOptions;
sourceOptions=function(source,module=current){if(source?.kind==='items')return sourceMembers(source).filter(i=>i.active&&genericItemAccess(i).some(a=>a.module===module));return genericBaseSourceOptions(source,module);};
itemVisibleConsumers=function(item){return genericItemAccess(item).map(a=>a.module).filter(m=>m!==item.module);};

function genericRestoreConsumerChecks(item){document.querySelectorAll('[data-item-share]').forEach(el=>{el.checked=(item?.shares||[]).includes(el.dataset.itemShare);});}

const genericHierarchyRender=renderHierarchyEditor;
renderHierarchyEditor=function(){
 genericHierarchyRender();const host=$('#hierarchyeditor');if(!host)return;
 host.insertAdjacentHTML('afterbegin','<p class="muted">Category access also applies to its items. Item links can add more areas.</p>');
 state.itemCategories.filter(c=>c.module===$('#hmodule').value).forEach(c=>{
  const row=$('#rename-'+c.id)?.closest('.inputrow');if(!row)return;
  row.insertAdjacentHTML('afterend',`<details class="hierarchy-module-links" data-hierarchy-id="${c.id}"><summary>${esc(c.name)} · departments (${(c.shares||[]).length})</summary><div class="item-share-grid">${modules.map(m=>`<label class="checkrow"><input type="checkbox" ${c.shares?.includes(m)?'checked':''} ${canEdit()?'':'disabled'} onchange="genericSetHierarchyLink('${c.id}','${m}',this.checked)">${m}</label>`).join('')}</div></details>`);
 });
};
function genericSetHierarchyLink(id,module,on,acknowledged=false){
 if(!canEdit())return;const group=itemCategory(id);if(!group)return;
 const affected=state.items.filter(i=>i.categoryId===id||i.subcategoryId===id);
 const references=!on?affected.flatMap(i=>itemUsages(i.id).map(u=>({...u,item:i.name}))):[];
 if(references.length&&!acknowledged){modal('Review department access',`<p>Remove ${esc(module)} from ${esc(group.name)}?</p><p>${references.length} work instruction steps use items in this group.</p><div class="actions"><button onclick="categoryModal()">Cancel</button><button class="primary" onclick="genericSetHierarchyLink('${id}','${module}',false,true);categoryModal()">Apply</button></div>`);return;}
 group.shares=(group.shares||[]).filter(m=>m!==module);if(on)group.shares.push(module);record('Updated category module links: '+group.name);genericUpdateHierarchyCaption(group);
}
function genericMovedSourceReferences(old,item){
 if(!old||old.module===item.module&&old.categoryId===item.categoryId)return [];
 const removed=ensureSources().filter(source=>source.kind==='items'&&source.owner===old.module&&source.categoryId===old.categoryId&&(source.owner!==item.module||source.categoryId!==item.categoryId));
 const references=[];
 for(const [module,workflow] of allWorkflowDrafts())for(const node of workflow.nodes||[]){
  const source=removed.find(source=>source.id===(node.catalogueSourceId||node.resourceSourceId));
  if(!source||!sourceOptions(source,module).some(i=>i.id===old.id))continue;
  if(node.answer==='Catalogue'||node.action==='Use shared resource'&&node.resourceId===old.id)references.push({module,title:workflow.title,nodeLabel:node.label,source:source.name});
 }
 return references;
}
const genericBaseCommitItem=commitItem;
commitItem=function(item){
 if(!canEdit()||!item)return;
 const old=itemById(item.id);
 const lost=old?genericItemAccess(old).map(a=>a.module).filter(m=>!genericItemAccess(item).some(a=>a.module===m)):[];
 const affected=old&&lost.length?itemUsages(old.id).filter(u=>lost.includes(u.module)):[];
 const moved=genericMovedSourceReferences(old,item);
 if(affected.length||moved.length){window.pendingHierarchyItem=item;modal(affected.length?'Review inherited access':'Review collection membership',`<p>${affected.length?'This removes access for '+lost.map(esc).join(', ')+' and affects '+affected.length+' work instruction steps. ':''}${moved.length?'Moving this item affects '+moved.length+' draft selections. ':''}</p>${moved.map(u=>`<div class="linkline"><span>${esc(u.title)} · ${esc(u.nodeLabel)}</span><span>${esc(u.module)} · ${esc(u.source)}</span></div>`).join('')}<div class="actions"><button onclick="itemModal('${old.id}')">Cancel</button><button class="primary" onclick="genericCommitInheritedItem()">Apply</button></div>`);return;}
 genericBaseCommitItem(item);
};
function genericCommitInheritedItem(){if(!canEdit()||!window.pendingHierarchyItem)return;const item=window.pendingHierarchyItem;window.pendingHierarchyItem=null;genericBaseCommitItem(item);}

function genericUpdateHierarchyCaption(group){
 const panel=[...document.querySelectorAll('[data-hierarchy-id]')].find(el=>el.dataset.hierarchyId===group.id);
 if(panel){const summary=panel.querySelector('summary');if(summary)summary.textContent=group.name+' · departments ('+(group.shares||[]).length+')';}
}
