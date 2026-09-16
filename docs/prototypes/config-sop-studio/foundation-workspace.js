/* CEO-facing foundation wrapper: keep three buckets clear. */
const foundationPreviousRender = render;
render = function () {
  foundationPreviousRender();
  if (current !== 'Common') return;
  document.querySelector('.tabs')?.remove();
  renderWorkInstructions();
};
function renderWorkInstructions(){
  const content=document.querySelector('#content');if(!content)return;
  const tools=['Question library','Action library','Dropdown lists','Data sources','Sharing'];
  const active=tools.includes(tab),body=active?content.innerHTML:'';
  const heading=document.querySelector('#main h1');if(heading)heading.textContent='Work instructions';
  const intro=document.querySelector('#main > .heading p');if(intro)intro.textContent='Choose a department, then create or update the steps its team follows.';
  const sopAreas=modules.filter(m=>m!=='Sales');
  content.innerHTML=`<section class="card"><h2>Choose the work area</h2><p>Choose the SOP area to review or change.</p><div class="actions">${sopAreas.map(m=>`<button onclick="go('${m}','SOPs')">${esc(m)} instructions →</button>`).join('')}</div></section><details class="card"><summary>Existing feature settings</summary><p>Use the existing product screens for specialist settings.</p><div class="actions"><button onclick="productionNavigate('Feed','Feed Config')">Feed settings</button><button onclick="productionNavigate('Health','Health Config')">Health protocols</button><button onclick="productionNavigate('Preventive Care','Vaccination plan')">Vaccination plan</button><button onclick="productionNavigate('Sales','Sales Config')">Sales settings</button></div></details><details class="card" ${active?'open':''}><summary>Admin authoring tools</summary><p>Reusable questions, actions and dropdown lists. Operators still use the existing mobile screens.</p><div class="actions"><button onclick="go('Common','Question library')">Question library</button><button onclick="go('Common','Action library')">Action library</button><button onclick="go('Common','Dropdown lists')">Dropdown lists</button></div>${body}</details>`;
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

const foundationRegistryRows = renderItemRows;
renderItemRows = function () {
  foundationRegistryRows();
  document.querySelectorAll('#itemrows td b, #itemrows .badge').forEach(el => {
    if (el.textContent === 'Common') { if(el.classList.contains('badge')) el.remove(); else el.textContent = 'Reusable item library'; }
  });
};
const foundationRegistry = renderItems;
renderItems = function () {
  foundationRegistry();
  const tree = document.querySelector('.items-tree');
  if (!tree) return;
  tree.querySelector('.items-tree-title').textContent = 'ITEMS AND SETTINGS FILTERS';
  const all = tree.querySelector('button');
  if (all) all.innerHTML = `All reusable items <span>${state.items.length}</span>`;
};
if (typeof listSelect === 'function') {
  const foundationListSelect = listSelect;
  listSelect = function(value,attr,kind) { return foundationListSelect(value,attr,kind).replaceAll('Common · ', 'Reusable item library · '); };
}
if (typeof sourceSelect === 'function') {
  const foundationSourceSelect = sourceSelect;
  sourceSelect = function(value,attr,kind) {
    return foundationSourceSelect(value,attr,kind).replace('<option value="">All owners</option>', `<option value="">All owners</option><option value="Common" ${sourcePickerFilters[attr]?.owner==='Common'?'selected':''}>Reusable item library</option>`).replaceAll('Common · ', 'Reusable item library · ');
  };
}
if (typeof showLists === 'function') {
  const foundationShowLists = showLists;
  showLists = function () {
    foundationShowLists();
    const note=document.querySelector('#content > .notice');
    if(note) note.textContent='Sharing a dropdown list makes its active records available to selected work areas. Published workflows keep their saved version.';
    document.querySelectorAll('.lists-grid .card').forEach(card=>{
      const title=card.querySelector('.eyebrow');
      if(title?.textContent.startsWith('Common ·')) title.textContent=title.textContent.replace('Common ·','Reusable item library ·');
    });
  };
}
if (typeof listModal === 'function') {
  const foundationListModal = listModal;
  listModal = function(id) {
    foundationListModal(id);
    const s=id?listById(id):null;
    if(s?.kind==='items'&&s.owner==='Common') {
      const desc=document.querySelector('.modal p');
      if(desc) desc.textContent='Reusable item choices for SOP dropdowns.';
    }
  };
}
if (typeof sourceModal === 'function') {
  const foundationSourceModal = sourceModal;
  sourceModal = function(id) {
    foundationSourceModal(id);
    const s=id?sourceById(id):null;
    if(s?.kind==='items'&&s.owner==='Common') {
      const desc=document.querySelector('.modal p');
      if(desc) desc.textContent='Reusable item choices for SOP dropdowns.';
    }
  };
}
const foundationCategoryModal = categoryModal;
categoryModal = function () {
  foundationCategoryModal();
  const desc=document.querySelector('.modal p');
  if(desc) desc.textContent='Reusable item categories.';
};
const foundationNavigate = go;
go = function (module, page) {
  foundationNavigate(module, page);
  window.scrollTo({top:0,left:0,behavior:'instant'});
};
const foundationItemPath = itemPath;
itemPath = function (item) { return foundationItemPath(item).replace(/^Common › /, 'Reusable item library › ').replace(/^Common \/ /, 'Reusable item library / '); };
const foundationInspect = inspect;
inspect = function () {
  foundationInspect();
  document.querySelectorAll('#inspector option').forEach(option => {
    if (option.textContent.endsWith(' — Common')) option.textContent=option.textContent.replace(/ — Common$/, ' — Reusable item library');
  });
};
