/* Shared navigation for the generic foundation. All writes remain browser-local. */
const foundationPreviousRender = render;
render = function () {
 foundationPreviousRender();
 if(current!=='Common')return;
 document.querySelector('.tabs')?.remove();
 renderWorkInstructions();
};
function renderWorkInstructions(){
 const content=document.querySelector('#content');if(!content)return;
 const tools=['Question library','Action library','Data sources','Sharing'];
 const active=tools.includes(tab),body=active?content.innerHTML:'';
 const heading=document.querySelector('#main h1');if(heading)heading.textContent='Work instructions';
 const intro=document.querySelector('#main > .heading p');if(intro)intro.textContent='Choose a department, then create or update the steps its team follows.';
 content.innerHTML=`<section class="card"><h2>Which department is this work for?</h2><p>Open its instructions to add questions, actions and checks in the order the team needs them.</p><div class="actions">${modules.map(m=>`<button onclick="go('${m}','SOPs')">${esc(m)} instructions →</button>`).join('')}</div></section><section class="card"><h2>Examples to review</h2><p>See how an existing SOP can bring together stages and their prerequisites. Start from a blank SOP or choose an example.</p><button onclick="window.openBusinessExamples()">View examples</button></section><details class="card"><summary>Existing department settings</summary><p>Keep specialist settings with the department that manages them.</p><div class="actions"><button onclick="productionNavigate('Feed','Feed Config')">Feed settings</button><button onclick="productionNavigate('Health','Health Config')">Health protocols</button><button onclick="productionNavigate('Preventive Care','Vaccination plan')">Vaccination plan</button><button onclick="productionNavigate('Sales','Sales Config')">Sales settings</button></div></details><p><a href="research/index.html" target="_blank" rel="noopener">Read the existing feature reference →</a></p><details class="card" ${active?'open':''}><summary>Saved questions, actions and lists</summary><p>Reuse these while building work instructions. Changes stay in this local prototype.</p><div class="actions"><button onclick="go('Common','Question library')">Saved questions</button><button onclick="go('Common','Action library')">Saved actions</button><button onclick="go('Common','Data sources')">Shared lists</button></div>${body}</details>`;
}
const foundationItemsRender = renderItems;
renderItems = function () {
  foundationItemsRender();
  if (current === 'Items') {
    const main = document.querySelector('#main');
    main.insertAdjacentHTML('afterbegin', `<div class="foundation-path" aria-label="Build with reusable tools"><span class="active">1 · Configure items</span><button onclick="go('Common','Work instructions')">2 · Write work instructions</button></div>`);
  }
};
function renderRunInsights(){renderWorkInstructions();}

// New generic records live in the shared catalogue; retain legacy module-owned groups.
const foundationRegistryRows = renderItemRows;
renderItemRows = function () {
  foundationRegistryRows();
  document.querySelectorAll('#itemrows td b, #itemrows .badge').forEach(el => {
    if (el.textContent === 'Common') { if(el.classList.contains('badge')) el.remove(); else el.textContent = 'Shared catalogue'; }
  });
};
const foundationRegistry = renderItems;
renderItems = function () {
  foundationRegistry();
  const tree = document.querySelector('.items-tree');
  if (!tree) return;
  tree.querySelector('.items-tree-title').textContent = 'CATALOGUE · CATEGORY · SUBCATEGORY';
  const all = tree.querySelector('button');
  if (all) all.innerHTML = `All items <span>${state.items.length}</span>`;
  const cats = state.itemCategories.filter(c => c.module === 'Common' && !c.parentId);
  all.insertAdjacentHTML('afterend', `<button class="${itemFilter.module === 'Common' ? 'active' : ''}" onclick="setItemFilter('module','Common')">Shared catalogue <span>${state.items.filter(i=>i.module==='Common').length}</span></button>${itemFilter.module==='Common'?cats.map(c=>`<button class="item-tree-cat ${itemFilter.category===c.id?'active':''}" onclick="setItemFilter('category','${c.id}')">↳ ${esc(c.name)}</button>${itemFilter.category===c.id?state.itemCategories.filter(s=>s.parentId===c.id).map(s=>`<button class="item-tree-sub ${itemFilter.subcategory===s.id?'active':''}" onclick="setItemFilter('subcategory','${s.id}')">${esc(s.name)}</button>`).join(''):''}`).join(''):''}`);
  const crumb = document.querySelector('.items-breadcrumb');
  if (crumb) crumb.textContent = `${itemFilter.module==='Common'?'Shared catalogue':itemFilter.module||'All items'}${itemFilter.category?' / '+itemCategoryName(itemFilter.category):''}${itemFilter.subcategory?' / '+itemCategoryName(itemFilter.subcategory):''}`;
  const stats=document.querySelector('.item-stats');
  if(stats) stats.innerHTML=`<span><b>${state.items.filter(i=>i.active).length}</b> active items</span><span><b>${state.items.filter(i=>i.module==='Common').length}</b> shared-catalogue items</span><span><b>${state.items.filter(i=>itemVisibleConsumers(i).length).length}</b> linked to modules</span>`;
};
const foundationSourceSelect = sourceSelect;
sourceSelect = function(value,attr,kind) {
  return foundationSourceSelect(value,attr,kind).replace('<option value="">All owners</option>', `<option value="">All owners</option><option value="Common" ${sourcePickerFilters[attr]?.owner==='Common'?'selected':''}>Shared catalogue</option>`).replaceAll('Common · ', 'Shared catalogue · ');
};
const foundationShowSources = showSources;
showSources = function () {
  foundationShowSources();
  const owner=document.querySelector('[aria-label="Source directory owner"]');
  if(owner) owner.insertAdjacentHTML('beforeend', `<option value="Common" ${sourceDirectoryOwner==='Common'?'selected':''}>Shared catalogue</option>`);
  const note=document.querySelector('#content > .notice');
  if(note) note.textContent='Category, subcategory, item and source module links combine. The same availability applies to SOP questions and actions. Published SOPs keep their snapshots.';
  document.querySelectorAll('.sources-grid .card').forEach(card=>{
    const title=card.querySelector('.eyebrow');
    if(!title?.textContent.startsWith('Common ·')) return;
    title.textContent=title.textContent.replace('Common ·','Shared catalogue ·');
    const ownerButton=Array.from(card.querySelectorAll('button')).find(b=>b.textContent==='Owner SOPs →');
    if(ownerButton){ownerButton.textContent='Configure item modules →';ownerButton.setAttribute('onclick',"go('Items');setItemFilter('module','Common')");}
  });
};
const foundationSourceModal = sourceModal;
sourceModal = function(id) {
  foundationSourceModal(id);
  const s=id?sourceById(id):null;
  if(s?.kind==='items'&&s.owner==='Common') {
    const desc=document.querySelector('.modal p');
    if(desc) desc.textContent='Items are grouped by this shared category. Category, subcategory, item and shared-source links combine; configure hierarchy links in Items Config.';
  }
};
const foundationCategoryModal = categoryModal;
categoryModal = function () {
  foundationCategoryModal();
  const owner=document.querySelector('#hmodule');
  if(owner){owner.insertAdjacentHTML('afterbegin','<option value="Common">Shared catalogue</option>');if(!itemFilter.module||itemFilter.module==='Common'){owner.value='Common';renderHierarchyEditor();}}
  const desc=document.querySelector('.modal p');
  if(desc) desc.textContent='Manage shared catalogue categories or existing module-owned groups. Renaming keeps stable item references.';
};
const foundationNavigate = go;
go = function (module, page) {
  foundationNavigate(module, page);
  window.scrollTo({top:0,left:0,behavior:'instant'});
};
const foundationItemPath = itemPath;
itemPath = function (item) { return foundationItemPath(item).replace(/^Common › /, 'Shared catalogue › '); };
const foundationInspect = inspect;
inspect = function () {
  foundationInspect();
  document.querySelectorAll('#inspector option').forEach(option => {
    if (option.textContent.endsWith(' — Common')) option.textContent=option.textContent.replace(/ — Common$/, ' — Shared catalogue');
  });
};
