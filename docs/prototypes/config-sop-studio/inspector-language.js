// Friendly question/condition wording; canvas remains the primary wiring surface.
const languageInspect=inspect;
inspect=function(){
 languageInspect();const panel=document.querySelector('#inspector');if(!panel)return;
 const n=sop().nodes.find(n=>n.id===selected);if(!n)return;
 const title=panel.querySelector('h2');if(title)title.textContent=({start:'Start',question:'Question',condition:'Branch rule',action:'Action',end:'Finish'})[n.type]||'Step';
 const op=panel.querySelector('[aria-label="Decision comparison"]');
 if(op){const names={'>':'is greater than','>=':'is at least','<':'is less than','<=':'is at most','=':'is','!=':'is not',contains:'includes'};for(const option of op.options){const value=option.value;option.value=value;option.textContent=names[value]||value;}}
 if(n.type==='condition'){
  const q=sop().nodes.find(q=>q.id===n.source);const v=panel.querySelector('[aria-label="Decision value"]');
  if(v&&v.tagName==='INPUT'&&q&&['Yes / No','Single choice','Multiple choice'].includes(q.answer)){
   const choices=q.answer==='Yes / No'?['Yes','No']:(q.choices||'').split(',').map(v=>v.trim()).filter(Boolean);
   const select=document.createElement('select');select.setAttribute('aria-label','Decision value');select.disabled=!canEdit();
   for(const choice of [...new Set([...(choices.includes(n.value)?[]:[n.value]),...choices])]){const option=document.createElement('option');option.value=choice;option.textContent=choice;option.selected=choice===n.value;select.append(option);}select.onchange=()=>editNode('value',select.value);v.replaceWith(select);
  }
 }
 const destinations=[...panel.querySelectorAll('select[aria-label$=" destination"]')];
 if(destinations.length){const details=document.createElement('details');details.className='advanced-connections';const summary=document.createElement('summary');summary.textContent='Advanced connections';details.append(summary);for(const select of destinations){const label=select.closest('label');if(label)details.append(label);}panel.append(details);}
};
validateDialog=function(){
 const errors=validateWorkflow(sop());
 modal(errors.length?'Fix these steps':'Ready to test',errors.length?`<p>Select an issue to jump to its step on the canvas.</p>${errors.map((error,index)=>{const matches=sop().nodes.filter(n=>error.startsWith(n.label+':'));return `<div class="notice error">${esc(error)} ${matches.map(n=>`<button data-validation-node="${esc(n.id)}">Show step →</button>`).join('')}</div>`;}).join('')}`:`<div class="notice">All paths are connected and required resources are available.</div>`);
 for(const button of document.querySelectorAll('[data-validation-node]'))button.onclick=()=>{selected=button.dataset.validationNode;closeModal();renderEditor();const node=document.querySelector('[data-node="'+selected+'"]');node?.scrollIntoView({block:'center',inline:'center'});};
};
