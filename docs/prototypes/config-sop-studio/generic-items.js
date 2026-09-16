/* Item-first presentation. Existing stable IDs, access rules and save impact checks remain authoritative. */
function genericItemAccess(item, sources=state.dataSources||[]) {
  const grants=new Map(),add=(module,reason)=>{if(!module||module==='Common')return;grants.set(module,[...(grants.get(module)||[]),reason]);};
  if(item.module!=='Common')add(item.module,'Maintaining department');
  const category=(state.itemCategories||[]).find(c=>c.id===item.categoryId),sub=(state.itemCategories||[]).find(c=>c.id===item.subcategoryId);
  (category?.shares||[]).forEach(m=>add(m,'Category: '+category.name));
  (sub?.shares||[]).forEach(m=>add(m,'Subcategory: '+sub.name));
  sources.filter(s=>s.kind==='items'&&s.owner===item.module&&s.categoryId===item.categoryId).forEach(s=>(s.shares||[]).forEach(m=>add(m,'Shared list: '+s.name)));
  (item.shares||[]).forEach(m=>add(m,'Item link'));
  return [...grants].map(([module,reasons])=>({module,selectable:!!item.active,reason:[...new Set(reasons)].join(' · ')}));
}
const genericBaseItems=renderItems;
renderItems=function(){genericBaseItems();const heading=$('.items-heading p');if(heading)heading.textContent='Add an item or setting once. Choose which departments can use it in their work instructions.';const stats=$('.item-stats');if(stats)stats.insertAdjacentHTML('afterend','<div class="notice generic-local-note">Mock catalogue · saved in this browser only. Production database and mobile integration are not connected.</div>');};
const genericBaseItemModal=itemModal;
itemModal=function(id){
  genericBaseItemModal(id);const form=$('#itemform');if(!form)return;
  const existing=id?itemById(id):null,isGeneric=!existing||existing.module==='Common';
  if(isGeneric){
    const moduleSelect=$('#imodule');moduleSelect.insertAdjacentHTML('afterbegin','<option value="Common">Shared catalogue</option>');moduleSelect.value='Common';moduleSelect.disabled=true;
    genericRestoreConsumerChecks(existing);
    refreshItemCategories(existing?.categoryId||'',existing?.subcategoryId||'');
  }
  $('.item-drawer-intro').innerHTML='Name the item or setting → choose departments → use it in work instructions.'+(id?'<br><span class="muted">Stable reference: '+esc(id)+' · Revision '+itemById(id).revision+'</span>':'');
  const name=$('#iname').closest('label'),owner=$('#imodule').closest('label'),unit=$('#iunit').closest('label'),ownerRow=owner.parentElement;
  const shares=$('#itemshares');
  // Replace static sharing copy with the live effective-access preview.
  [...form.children].filter(el=>(el.tagName==='SPAN'&&el.textContent==='Share with other departments')||(el.tagName==='P'&&el.textContent.startsWith('The maintaining department'))||(el.classList.contains('notice')&&el.textContent.startsWith('Additional access through shared sources:'))).forEach(el=>el.remove());
  const access=document.createElement('section');access.className='generic-access';access.innerHTML='<h3>Where can this item be used?</h3><p class="muted">'+(isGeneric?'Choose the departments that need this item or setting. Each selected team can use it in their work instructions.':'Choose the departments that need this item. Its maintaining department and category links also apply.')+'</p>';access.appendChild(shares);name.after(access);
  const basics=document.createElement('div');basics.className='inputrow';basics.appendChild(name);basics.appendChild(unit);form.prepend(basics);
  owner.querySelector('span').textContent=isGeneric?'Catalogue':'Maintained by';ownerRow.classList.add('generic-maintenance');ownerRow.insertAdjacentHTML('afterbegin','<p class="muted">'+(isGeneric?'Group similar items by category and subcategory: Electrical appliances → Fans, Clothes → Sarees, or Supplies → Needles.':'This item keeps its maintaining department and category. That department can always use it.')+'</p>');
  const categoryRow=$('#icategory').closest('.inputrow');categoryRow.insertAdjacentHTML('afterend',`<details class="generic-category-create"><summary>Add a category without leaving this item</summary><p class="muted">Your item draft stays here. New groups are saved locally immediately.</p><div class="inputrow"><label class="field"><span>New category</span><input id="generic-category-name" maxlength="100" placeholder="e.g. Consumables" ${canEdit()?'':'disabled'}></label><button type="button" onclick="genericCreateCategory(false)" ${canEdit()?'':'disabled'}>Add category</button></div><div class="inputrow"><label class="field"><span>New subcategory in selected category</span><input id="generic-subcategory-name" maxlength="100" placeholder="e.g. Needles and syringes" ${canEdit()?'':'disabled'}></label><button type="button" onclick="genericCreateCategory(true)" ${canEdit()?'':'disabled'}>Add subcategory</button></div><p id="generic-category-feedback" role="status"></p></details>`);
  access.insertAdjacentHTML('beforeend','<div id="generic-access-preview" aria-live="polite"></div>');
  form.addEventListener('input',genericRefreshPreview);form.addEventListener('change',genericRefreshPreview);
  $('.item-drawer-footer').insertAdjacentHTML('beforebegin','<p class="muted generic-save-note">Save keeps this item in this browser across reloads. This mock does not write to the production database.</p>');genericRefreshPreview();
};
function genericRefreshPreview(){
 const preview=$('#generic-access-preview');if(!preview)return;
 const draft={name:$('#iname').value.trim()||'Your item',module:$('#imodule').value,categoryId:$('#icategory').value,subcategoryId:$('#isubcategory').value,active:$('#iactive').value==='true',shares:[...document.querySelectorAll('[data-item-share]:checked')].map(el=>el.dataset.itemShare)};
 const access=genericItemAccess(draft);preview.innerHTML='<h4>Departments that can use this</h4><p class="muted">'+(draft.active?'After saving, these departments can choose this item or setting.':'Archived items cannot be chosen for new work. Previously published instructions keep their saved version.')+'</p><p class="muted">'+(draft.module==='Common'?'Choose at least one department, here or on its category.':'The maintaining department can always use this.')+(draft.module==='Common'?' Departments chosen on a category, subcategory or shared list also apply. To stop sharing, remove each applicable link.':' To change shared-list links, open Work instructions → Saved questions, actions and lists → Shared lists. Category links also apply.')+'</p><div class="generic-module-previews">'+(access.length?'':'<p class="muted">Choose a department on this item, its category or subcategory.</p>')+access.map(a=>'<div><strong>'+esc(a.module)+'</strong><span class="muted">'+esc(a.reason)+'</span><select aria-label="'+esc(a.module)+' item preview" disabled><option>'+esc(a.selectable?draft.name:'Not selectable · archived')+'</option></select></div>').join('')+'</div>';
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
 host.insertAdjacentHTML('afterbegin','<p class="muted">Departments selected on a category also apply to its items. Subcategory and item links add more departments. Removing one link keeps any other links; published instructions keep their saved version.</p>');
 state.itemCategories.filter(c=>c.module===$('#hmodule').value).forEach(c=>{
  const row=$('#rename-'+c.id)?.closest('.inputrow');if(!row)return;
  row.insertAdjacentHTML('afterend',`<details class="hierarchy-module-links" data-hierarchy-id="${c.id}"><summary>${esc(c.name)} · departments (${(c.shares||[]).length})</summary><div class="item-share-grid">${modules.map(m=>`<label class="checkrow"><input type="checkbox" ${c.shares?.includes(m)?'checked':''} ${canEdit()?'':'disabled'} onchange="genericSetHierarchyLink('${c.id}','${m}',this.checked)">${m}</label>`).join('')}</div></details>`);
 });
};
function genericSetHierarchyLink(id,module,on,acknowledged=false){
 if(!canEdit())return;const group=itemCategory(id);if(!group)return;
 const affected=state.items.filter(i=>i.categoryId===id||i.subcategoryId===id);
 const references=!on?affected.flatMap(i=>itemUsages(i.id).map(u=>({...u,item:i.name}))):[];
 if(references.length&&!acknowledged){modal('Review department access',`<p>Remove ${esc(module)} from ${esc(group.name)}?</p><p>${references.length} work instruction steps use items in this group. Other department links still apply. Check drafts after removing a link; published instructions stay unchanged.</p><div class="actions"><button onclick="categoryModal()">Cancel</button><button class="primary" onclick="genericSetHierarchyLink('${id}','${module}',false,true);categoryModal()">Apply</button></div>`);return;}
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
 if(affected.length||moved.length){window.pendingHierarchyItem=item;modal(affected.length?'Review inherited access':'Review collection membership',`<p>${affected.length?'This change removes effective access for '+lost.map(esc).join(', ')+' and affects '+affected.length+' work instruction steps. ':''}${moved.length?'Moving this item removes it from '+moved.length+' source-backed draft selections. ':''}Drafts may need review. Previously published instructions keep their saved items.</p>${moved.map(u=>`<div class="linkline"><span>${esc(u.title)} · ${esc(u.nodeLabel)}</span><span>${esc(u.module)} · ${esc(u.source)}</span></div>`).join('')}<div class="actions"><button onclick="itemModal('${old.id}')">Cancel</button><button class="primary" onclick="genericCommitInheritedItem()">Apply</button></div>`);return;}
 genericBaseCommitItem(item);
};
function genericCommitInheritedItem(){if(!canEdit()||!window.pendingHierarchyItem)return;const item=window.pendingHierarchyItem;window.pendingHierarchyItem=null;genericBaseCommitItem(item);}

function genericUpdateHierarchyCaption(group){
 const panel=[...document.querySelectorAll('[data-hierarchy-id]')].find(el=>el.dataset.hierarchyId===group.id);
 if(panel){const summary=panel.querySelector('summary');if(summary)summary.textContent=group.name+' · departments ('+(group.shares||[]).length+')';}
}
