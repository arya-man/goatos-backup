/* Navigate a large source questionnaire without shrinking all fields to unreadable text. */
let focusedSopPage='';
function sourcePages(){const pages=[];for(const n of sop().nodes)if(n.sourcePage&&!pages.some(p=>p.id===n.sourcePage))pages.push({id:n.sourcePage,title:n.sourcePageTitle||n.sourcePage});return pages}
function focusSopPage(id){focusedSopPage=id;const nodes=sop().nodes.filter(n=>n.sourcePage===id);if(!nodes.length)return;selected=nodes[0].id;drawGraph();inspect();if(questionnaireView==='list'){const index=questionnaireOrder(sop().nodes).findIndex(n=>n.sourcePage===id);const row=document.querySelectorAll('.question-list-row')[index],container=$('.canvas');if(row&&container)container.scrollTop=row.getBoundingClientRect().top-container.getBoundingClientRect().top+container.scrollTop;updatePageNav();return}const c=$('.canvas');canvasView.scale=.85;canvasView.x=Math.max(25,(c.clientWidth-560*.85)/2)-nodes[0].x*.85;canvasView.y=35-nodes[0].y*.85;canvasTransform();updatePageNav()}
function updatePageNav(){const select=$('#source-page-select');if(select)select.value=focusedSopPage}
const pageEditorBase=renderEditor;renderEditor=function(){pageEditorBase();const pages=sourcePages();if(!pages.length||!$('.toolbar'))return;$('.toolbar').insertAdjacentHTML('afterend',`<div class="source-page-nav"><label>Jump to section <select id="source-page-select" aria-label="Questionnaire section" onchange="focusSopPage(this.value)">${pages.map(p=>`<option value="${esc(p.id)}">${esc(p.title)} · ${sop().nodes.filter(n=>n.sourcePage===p.id).length} questions</option>`).join('')}</select></label><span>${sop().nodes.filter(n=>n.type==='question'&&(n.sourceQuestionId||n.sourcePage)).length} source questions · drag the canvas to explore · List shows every field</span></div>`);if(!pages.some(p=>p.id===focusedSopPage))focusedSopPage=pages[0].id;focusSopPage(focusedSopPage);if(questionnaireView==='list'){document.querySelectorAll('[aria-label="Zoom out"],[aria-label="Zoom in"],#canvaszoom,.canvas-instruction').forEach(e=>e.style.display='none');const fit=Array.from(document.querySelectorAll('.toolbar button')).find(b=>b.textContent==='Fit');if(fit)fit.style.display='none'}updatePageNav()};

function pageGroupRole(pageId,title=''){
 const label=String(title||pageId||'').toLowerCase();
 if(current==='Weighing'){
  if(pageId==='weighing')return 'Operator captures · Verifier closes';
 }
 if(current==='Procurement'){
  if(pageId==='load')return 'Procurement desk';
  if(/verdict|decision|approve|reject/.test(label))return 'Field verdict · CEO/CXO approval follows';
  if(/identity|body|face|udder|testicle|teeth|visual|productivity/.test(label))return 'Field inspector';
 }
 if(current==='Feed')return 'Operator proof capture';
 return 'Operator';
}
function renderSourcePageGroups(){
 const graph=$('#graph');
 if(!graph||questionnaireView==='list')return;
 graph.querySelectorAll('.source-page-group-box').forEach(e=>e.remove());
 const nodes=sop().nodes.filter(n=>n.sourcePage&&n.type==='question');
 if(!nodes.length)return;
 const pages=[];
 for(const n of nodes){
  let page=pages.find(p=>p.id===n.sourcePage);
  if(!page){page={id:n.sourcePage,title:n.sourcePageTitle||n.sourcePage,scope:n.sourceScope||'animal',nodes:[]};pages.push(page)}
  page.nodes.push(n);
 }
 for(const p of pages){
  const minX=Math.min(...p.nodes.map(n=>n.x))-26;
  const minY=Math.min(...p.nodes.map(n=>n.y))-52;
  const maxX=Math.max(...p.nodes.map(n=>n.x+260))+26;
  const maxY=Math.max(...p.nodes.map(n=>n.y+canvasNodeHeight(n)))+28;
  const box=document.createElement('section');
  box.className='source-page-group-box';
  box.style.left=minX+'px';box.style.top=minY+'px';box.style.width=(maxX-minX)+'px';box.style.height=(maxY-minY)+'px';
  box.innerHTML=`<div class="source-page-group-head"><span>${esc(p.title)}</span><b>${p.nodes.length} question${p.nodes.length===1?'':'s'}</b></div><div class="source-page-group-meta">${esc(p.scope)} page · ${esc(pageGroupRole(p.id,p.title))}</div>`;
  graph.insertBefore(box,graph.firstChild?.nextSibling||graph.firstChild);
 }
}
const pageGroupDrawGraph=drawGraph;
drawGraph=function(){pageGroupDrawGraph();renderSourcePageGroups();};
if(typeof exposeGlobals==='function')exposeGlobals();
